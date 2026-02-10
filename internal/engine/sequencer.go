package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// ReorgEvent 表示检测到的 reorg 事件
type ReorgEvent struct {
	At *big.Int // reorg 发生的高度
}

// Sequencer 确保区块按顺序处理，解决并发抓取导致的乱序问题
type Sequencer struct {
	expectedBlock *big.Int             // 下一个期望处理的区块号
	buffer        map[string]BlockData // 区块号 -> 数据的缓冲区
	processor     *Processor           // 实际处理器
	fetcher       *Fetcher             // 用于Reorg时暂停抓取
	mu            sync.RWMutex         // 保护buffer和expectedBlock
	resultCh      <-chan BlockData     // 输入channel
	fatalErrCh    chan<- error         // 致命错误通知channel
	reorgCh       chan<- ReorgEvent    // reorg 事件通知channel
	chainID       int64                // 链ID用于checkpoint
	metrics       *Metrics             // Prometheus metrics

	lastProgressAt time.Time // 上次处理成功的时刻
	gapFillCount   int       // 连续 gap-fill 尝试次数（防止无限重试）
}

func NewSequencer(processor *Processor, startBlock *big.Int, chainID int64, resultCh <-chan BlockData, fatalErrCh chan<- error, metrics *Metrics) *Sequencer {
	return &Sequencer{
		expectedBlock:  new(big.Int).Set(startBlock),
		buffer:         make(map[string]BlockData),
		processor:      processor,
		resultCh:       resultCh,
		fatalErrCh:     fatalErrCh,
		chainID:        chainID,
		metrics:        metrics,
		lastProgressAt: time.Now(),
	}
}

func NewSequencerWithFetcher(processor *Processor, fetcher *Fetcher, startBlock *big.Int, chainID int64, resultCh <-chan BlockData, fatalErrCh chan<- error, reorgCh chan<- ReorgEvent, metrics *Metrics) *Sequencer {
	return &Sequencer{
		expectedBlock:  new(big.Int).Set(startBlock),
		buffer:         make(map[string]BlockData),
		processor:      processor,
		fetcher:        fetcher,
		resultCh:       resultCh,
		fatalErrCh:     fatalErrCh,
		reorgCh:        reorgCh,
		chainID:        chainID,
		metrics:        metrics,
		lastProgressAt: time.Now(),
	}
}

func (s *Sequencer) Run(ctx context.Context) {
	Logger.Info("🚀 Sequencer started. Expected block: " + s.expectedBlock.String())

	stallTicker := time.NewTicker(30 * time.Second)
	defer stallTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-stallTicker.C:
			// 巡检：如果停留在同一个块超过 10s，说明可能遇到了哈希洞或逻辑死锁
			s.mu.RLock()
			expectedStr := s.expectedBlock.String()
			expectedCopy := new(big.Int).Set(s.expectedBlock)
			_, hasExpected := s.buffer[expectedStr]
			bufferLen := len(s.buffer)
			idleTime := time.Since(s.lastProgressAt)

			// 扫描 buffer 找到最小的已缓冲区块号，确定 gap 范围
			var minBuffered *big.Int
			for numStr := range s.buffer {
				if n, ok := new(big.Int).SetString(numStr, 10); ok {
					if minBuffered == nil || n.Cmp(minBuffered) < 0 {
						minBuffered = n
					}
				}
			}
			s.mu.RUnlock()

			if idleTime > 30*time.Second {
				if bufferLen > 0 && !hasExpected {
					// 🚨 发现幽灵空洞：缓冲区有后面块但没当前块
					// 计算需要补齐的范围: [expected, minBuffered-1]
					gapEnd := new(big.Int).Sub(minBuffered, big.NewInt(1))
					gapSize := new(big.Int).Sub(minBuffered, expectedCopy).Int64()
					Logger.Error("🚨 CRITICAL_GAP_DETECTED",
						slog.String("missing_from", expectedStr),
						slog.String("missing_to", gapEnd.String()),
						slog.Int64("gap_size", gapSize),
						slog.Int("buffered_blocks", bufferLen),
						slog.Int("gap_fill_attempt", s.gapFillCount+1),
					)

					// 触发自愈：强制 Fetcher 批量重新调度所有缺失的块
					if s.fetcher != nil && s.gapFillCount < 10 {
						Logger.Info("🛡️ SELF_HEALING: Triggering batch gap-fill",
							slog.String("from", expectedStr),
							slog.String("to", gapEnd.String()),
						)
						go s.fetcher.Schedule(ctx, expectedCopy, gapEnd)
						s.gapFillCount++
					}
								} else {
									Logger.Warn("⚠️ SEQUENCER_STALLED_DETECTED", 
										slog.String("expected", expectedStr),
										slog.Int("buffer_size", bufferLen),
										slog.Duration("idle_time", idleTime),
									)
									if expectedStr == "1" || expectedStr == "0" {
										Logger.Info("💡 SRE_HINT: Indexer is healthy but upstream chain is idle. Please check if Anvil is mining or run 'python3 scripts/stress-test.py' to generate traffic.")
									}
								}
				
			}

		case data, ok := <-s.resultCh:
			if !ok {
				s.drainBuffer(ctx)
				return
			}

			// 收集一个批次的连续区块进行批量处理
			batch := []BlockData{data}
			maxBatchSize := 100

			// 给予一个小小的等待时间（10ms），让更多块进入 channel
			// 这能显著提升批量处理的机会
			timeout := time.After(10 * time.Millisecond)

		collect_loop:
			for len(batch) < maxBatchSize {
				select {
				case nextData, ok := <-s.resultCh:
					if !ok {
						break collect_loop
					}
					batch = append(batch, nextData)
				case <-timeout:
					break collect_loop
				default:
					if len(batch) > 0 {
						// 如果已经有数据了，且目前没新数据，稍微等一下或者直接出场
						// 这里我们选择直接出场，由 timeout 保证最低等待
					}
				}
			}

			// 关键优化：对批次进行排序，以最大化顺序处理的可能性
			// 因为并发 fetcher 会导致乱序到达
			sort.Slice(batch, func(i, j int) bool {
				n1 := batch[i].Number
				if n1 == nil && batch[i].Block != nil {
					n1 = batch[i].Block.Number()
				}
				n2 := batch[j].Number
				if n2 == nil && batch[j].Block != nil {
					n2 = batch[j].Block.Number()
				}

				if n1 == nil {
					return true
				} // nil first (error handling)
				if n2 == nil {
					return false
				}

				return n1.Cmp(n2) < 0
			})

			if err := s.handleBatch(ctx, batch); err != nil {
				select {
				case s.fatalErrCh <- err:
				case <-ctx.Done():
				}
				return
			}
		}
	}
}

func (s *Sequencer) handleBatch(ctx context.Context, batch []BlockData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 背压控制：如果缓冲区过大，暂停 Fetcher
	if s.fetcher != nil && len(s.buffer) > 800 && !s.fetcher.IsPaused() {
		Logger.Warn("⚠️ sequencer_buffer_high_pausing_fetcher", slog.Int("buffer_size", len(s.buffer)))
		s.fetcher.Pause()
	}

	i := 0
	for i < len(batch) {
		data := batch[i]
		blockNum := data.Number
		if blockNum == nil && data.Block != nil {
			blockNum = data.Block.Number()
		}

		// 尝试批量顺序处理
		// 只有当当前块没有错误时才尝试批量，否则走单条处理以触发重试逻辑
		if blockNum != nil && blockNum.Cmp(s.expectedBlock) == 0 && data.Err == nil {
			sequentialBatch := []BlockData{data}
			nextExpected := new(big.Int).Add(s.expectedBlock, big.NewInt(1))

			j := i + 1
			for j < len(batch) {
				nextData := batch[j]
				// 如果发现错误，立即停止批次收集，确保错误块通过 handleBlockLocked 处理
				if nextData.Err != nil {
					break
				}

				nNum := nextData.Number
				if nNum == nil && nextData.Block != nil {
					nNum = nextData.Block.Number()
				}

				if nNum != nil && nNum.Cmp(nextExpected) == 0 {
					sequentialBatch = append(sequentialBatch, nextData)
					nextExpected.Add(nextExpected, big.NewInt(1))
					j++
				} else {
					break
				}
			}

			if len(sequentialBatch) > 1 {
				Logger.Info("sequencer_processing_batch",
					slog.Int("size", len(sequentialBatch)),
					slog.String("from", sequentialBatch[0].Number.String()),
					slog.String("to", sequentialBatch[len(sequentialBatch)-1].Number.String()),
				)
				if err := s.processor.ProcessBatch(ctx, sequentialBatch, s.chainID); err != nil {
					return err
				}
				s.expectedBlock.Set(nextExpected)
				i = j
				s.processBufferContinuationsLocked(ctx)
				continue
			}
		}

		if err := s.handleBlockLocked(ctx, data); err != nil {
			return err
		}
		i++
	}
	return nil
}

func (s *Sequencer) handleBlock(ctx context.Context, data BlockData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleBlockLocked(ctx, data)
}

func (s *Sequencer) handleBlockLocked(ctx context.Context, data BlockData) error {
	blockNum := data.Number
	if blockNum == nil && data.Block != nil {
		blockNum = data.Block.Number()
	}

	if data.Err != nil {
		Logger.Warn("sequencer_fetch_error_retrying", slog.String("block", blockNum.String()))
		if blockNum != nil {
			var err error
			data.Block, err = s.processor.client.BlockByNumber(ctx, blockNum)
			if err == nil {
				q := ethereum.FilterQuery{FromBlock: blockNum, ToBlock: blockNum, Topics: [][]common.Hash{{TransferEventHash}}}
				data.Logs, err = s.processor.client.FilterLogs(ctx, q)
				if err == nil {
					data.Err = nil
					Logger.Info("sequencer_retry_success", slog.String("block", blockNum.String()))
				}
			}
		}
		if data.Err != nil {
			return fmt.Errorf("fetch error for block %s: %w", blockNum.String(), data.Err)
		}
	}

	blockNum = data.Block.Number()
	if blockNum.Cmp(s.expectedBlock) == 0 {
		if err := s.processSequentialLocked(ctx, data); err != nil {
			return err
		}
		s.processBufferContinuationsLocked(ctx)
		return nil
	}

	if blockNum.Cmp(s.expectedBlock) < 0 {
		return nil
	}

	s.buffer[blockNum.String()] = data
	if len(s.buffer) > 1000 {
		return fmt.Errorf("sequencer buffer overflow: %d blocks", len(s.buffer))
	}
	return nil
}

func (s *Sequencer) processSequentialLocked(ctx context.Context, data BlockData) error {
	if err := s.processor.ProcessBlockWithRetry(ctx, data, 3); err != nil {
		if _, ok := err.(ReorgError); ok {
			return s.handleReorgLocked(ctx, data)
		}
		return err
	}
	s.expectedBlock.Add(s.expectedBlock, big.NewInt(1))
	s.lastProgressAt = time.Now() // 💡 成功推进，重置计时
	s.gapFillCount = 0            // 重置 gap-fill 计数器
	return nil
}

func (s *Sequencer) processBufferContinuationsLocked(ctx context.Context) {
	for {
		nextNumStr := s.expectedBlock.String()
		data, exists := s.buffer[nextNumStr]
		if !exists {
			break
		}
		delete(s.buffer, nextNumStr)
		if err := s.processSequentialLocked(ctx, data); err != nil {
			s.buffer[nextNumStr] = data
			break
		}
	}

	// 缓冲区降至安全水位，恢复 Fetcher
	if s.fetcher != nil && len(s.buffer) < 200 && s.fetcher.IsPaused() {
		Logger.Info("✅ sequencer_buffer_low_resuming_fetcher", slog.Int("buffer_size", len(s.buffer)))
		s.fetcher.Resume()
	}
}

func (s *Sequencer) handleReorgLocked(ctx context.Context, data BlockData) error {
	blockNum := data.Block.Number()
	if s.fetcher != nil {
		s.fetcher.Pause()
	}
	for numStr := range s.buffer {
		num, _ := new(big.Int).SetString(numStr, 10)
		if num.Cmp(blockNum) >= 0 {
			delete(s.buffer, numStr)
		}
	}
	s.expectedBlock.Set(blockNum)
	if s.reorgCh != nil {
		select {
		case s.reorgCh <- ReorgEvent{At: new(big.Int).Set(blockNum)}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ErrReorgNeedRefetch
}

func (s *Sequencer) drainBuffer(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processBufferContinuationsLocked(ctx)
}

func (s *Sequencer) GetExpectedBlock() *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return new(big.Int).Set(s.expectedBlock)
}

func (s *Sequencer) GetBufferSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buffer)
}
