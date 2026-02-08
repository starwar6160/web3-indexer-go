package engine

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
)

// ReorgEvent 表示检测到的 reorg 事件
type ReorgEvent struct {
	At *big.Int // reorg 发生的高度
}

// Sequencer 确保区块按顺序处理，解决并发抓取导致的乱序问题
type Sequencer struct {
	expectedBlock *big.Int                    // 下一个期望处理的区块号
	buffer        map[string]BlockData        // 区块号 -> 数据的缓冲区 (使用string作为key避免big.Int比较问题)
	processor     *Processor                  // 实际处理器
	fetcher       *Fetcher                    // 用于Reorg时暂停抓取
	mu            sync.RWMutex                // 保护buffer和expectedBlock
	resultCh      <-chan BlockData            // 输入channel
	fatalErrCh    chan<- error                // 致命错误通知channel
	reorgCh       chan<- ReorgEvent           // reorg 事件通知channel（可选）
	chainID       int64                       // 链ID用于checkpoint
	metrics       *Metrics                    // Prometheus metrics
}

func NewSequencer(processor *Processor, startBlock *big.Int, chainID int64, resultCh <-chan BlockData, fatalErrCh chan<- error, metrics *Metrics) *Sequencer {
	return &Sequencer{
		expectedBlock: new(big.Int).Set(startBlock),
		buffer:        make(map[string]BlockData),
		processor:     processor,
		resultCh:      resultCh,
		fatalErrCh:    fatalErrCh,
		chainID:       chainID,
		metrics:       metrics,
	}
}

// NewSequencerWithFetcher 创建带 Fetcher 控制的 Sequencer（推荐用于 Reorg 处理）
func NewSequencerWithFetcher(processor *Processor, fetcher *Fetcher, startBlock *big.Int, chainID int64, resultCh <-chan BlockData, fatalErrCh chan<- error, reorgCh chan<- ReorgEvent, metrics *Metrics) *Sequencer {
	return &Sequencer{
		expectedBlock: new(big.Int).Set(startBlock),
		buffer:        make(map[string]BlockData),
		processor:     processor,
		fetcher:       fetcher,
		resultCh:      resultCh,
		fatalErrCh:    fatalErrCh,
		reorgCh:       reorgCh,
		chainID:       chainID,
		metrics:       metrics,
	}
}

// Run 启动排序处理器，按顺序处理区块
func (s *Sequencer) Run(ctx context.Context) {
	log.Printf("Sequencer started. Expected block: %s", s.expectedBlock.String())
	
	for {
		select {
		case <-ctx.Done():
			log.Printf("Sequencer shutting down. Buffer size: %d", len(s.buffer))
			return
			
		case data, ok := <-s.resultCh:
			if !ok {
				// channel关闭，尝试清空buffer
				s.drainBuffer(ctx)
				return
			}
			
			if err := s.handleBlock(ctx, data); err != nil {
				// 致命错误，通知外层关闭
				select {
				case s.fatalErrCh <- err:
				case <-ctx.Done():
				}
				return
			}
		}
	}
}

// handleBlock 处理单个区块数据，维护顺序性
func (s *Sequencer) handleBlock(ctx context.Context, data BlockData) error {
	if data.Err != nil {
		return fmt.Errorf("fetch error for block: %w", data.Err)
	}
	
	blockNum := data.Block.Number()
	blockNumStr := blockNum.String()
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// 如果这个区块正是我们期望的下一个，立即处理
	if blockNum.Cmp(s.expectedBlock) == 0 {
		if err := s.processSequential(ctx, data); err != nil {
			return err
		}
		
		// 检查buffer中是否有连续的后续区块可以处理
		s.processBufferContinuations(ctx)
		return nil
	}
	
	// 如果区块号小于期望的，说明已经处理过了，跳过
	if blockNum.Cmp(s.expectedBlock) < 0 {
		log.Printf("Skipping duplicate/late block %s (expected %s)", blockNumStr, s.expectedBlock.String())
		return nil
	}
	
	// 区块号大于期望的，存入buffer等待
	log.Printf("Buffering out-of-order block %s (expected %s). Buffer size: %d", 
		blockNumStr, s.expectedBlock.String(), len(s.buffer)+1)
	s.buffer[blockNumStr] = data
	
	// Update metrics
	bufferSize := len(s.buffer)
	s.metrics.UpdateSequencerBufferSize(bufferSize)
	
	// 分级告警：buffer 膨胀
	if bufferSize > 500 {
		LogBufferFull(bufferSize, s.expectedBlock.String())
		s.metrics.RecordSequencerBufferFull()
	}
	
	// 如果buffer过大，可能是前面的区块丢失了，需要致命告警
	if bufferSize > 1000 {
		return fmt.Errorf("sequencer buffer overflow: %d blocks pending", bufferSize)
	}
	
	return nil
}

// processSequential 按顺序处理区块并更新checkpoint
func (s *Sequencer) processSequential(ctx context.Context, data BlockData) error {
	blockNum := data.Block.Number()
	
	// 执行处理（带重试）
	if err := s.processor.ProcessBlockWithRetry(ctx, data, 3); err != nil {
		// 区分reorg错误和致命错误
		if _, ok := err.(ReorgError); ok {
			// reorg需要特殊处理：清空buffer，重置expected block，发送事件
			return s.handleReorg(ctx, data)
		}
		return fmt.Errorf("failed to process block %s: %w", blockNum.String(), err)
	}
	
	// 成功处理后推进期望值
	s.expectedBlock.Add(s.expectedBlock, big.NewInt(1))
	return nil
}

// processBufferContinuations 处理buffer中连续的区块
func (s *Sequencer) processBufferContinuations(ctx context.Context) {
	for {
		nextNumStr := s.expectedBlock.String()
		data, exists := s.buffer[nextNumStr]
		if !exists {
			break
		}
		
		delete(s.buffer, nextNumStr)
		
		if err := s.processSequential(ctx, data); err != nil {
			// 记录错误但不中断，让外层通过channel处理
			log.Printf("Error processing buffered block %s: %v", nextNumStr, err)
			// 放回buffer稍后重试
			s.buffer[nextNumStr] = data
			break
		}
		
		log.Printf("Processed buffered block %s", nextNumStr)
		LogBlockProcessing(nextNumStr, data.Block.Hash().Hex(), 0)
	}
}

// handleReorg 处理重组事件
func (s *Sequencer) handleReorg(ctx context.Context, data BlockData) error {
	blockNum := data.Block.Number()
	
	// 暂停 Fetcher 防止继续写入旧分叉数据
	if s.fetcher != nil {
		s.fetcher.Pause()
	}
	
	// 清空所有大于等于当前区块的buffer数据（这些可能是旧分叉的数据）
	toDelete := []string{}
	for numStr := range s.buffer {
		num := new(big.Int)
		num.SetString(numStr, 10)
		if num.Cmp(blockNum) >= 0 {
			toDelete = append(toDelete, numStr)
		}
	}
	for _, numStr := range toDelete {
		delete(s.buffer, numStr)
	}
	
	// 重置expected block到reorg点
	s.expectedBlock.Set(blockNum)
	
	// 发送 reorg 事件给 main（如果有 reorgCh）
	if s.reorgCh != nil {
		select {
		case s.reorgCh <- ReorgEvent{At: new(big.Int).Set(blockNum)}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	
	// 返回特殊错误，通知外层需要重新调度fetch
	return ErrReorgNeedRefetch
}

// drainBuffer 尝试在关闭前清空buffer
func (s *Sequencer) drainBuffer(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	for len(s.buffer) > 0 {
		nextNumStr := s.expectedBlock.String()
		data, exists := s.buffer[nextNumStr]
		if !exists {
			log.Printf("Cannot drain buffer: missing block %s, remaining: %d", nextNumStr, len(s.buffer))
			return
		}
		
		delete(s.buffer, nextNumStr)
		
		if err := s.processor.ProcessBlockWithRetry(ctx, data, 3); err != nil {
			log.Printf("Failed to drain block %s: %v", nextNumStr, err)
			return
		}
		
		s.expectedBlock.Add(s.expectedBlock, big.NewInt(1))
	}
}

// GetExpectedBlock 返回当前期望的区块号（用于外部监控）
func (s *Sequencer) GetExpectedBlock() *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return new(big.Int).Set(s.expectedBlock)
}

// GetBufferSize 返回当前缓冲区大小（用于外部监控）
func (s *Sequencer) GetBufferSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buffer)
}
