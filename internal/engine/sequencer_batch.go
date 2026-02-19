package engine

import (
	"context"
	"log/slog"
	"math/big"

	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func (s *Sequencer) handleBatch(ctx context.Context, batch []BlockData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
	defer func() {
		dur := time.Since(start)
		if dur > 500*time.Millisecond {
			slog.Warn("⚠️ Sequencer: SLOW BATCH PROCESSING",
				"size", len(batch),
				"dur", dur)
		}
	}()

	// 背压控制：如果缓冲区过大，暂停 Fetcher
	if s.fetcher != nil && len(s.buffer) > 2000 && !s.fetcher.IsPaused() {
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
			// 如果返回错误，说明是真正的不可恢复错误
			return err
		}
		// 即使 handleBlockLocked 返回 nil (临时抓取失败)，我们也继续处理批次中的其它块
		// 该块会留在 buffer 中等待下次调度或自愈。
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
	blockNum := s.resolveBlockNum(data)
	blockLabel := s.blockLabel(blockNum)

	if s.handleRangeTeleportLocked(ctx, data, blockNum) {
		return nil
	}

	data = s.retryFetchIfNeededLocked(ctx, data, blockNum, blockLabel)
	if data.Err != nil {
		return nil
	}

	if !s.hydrateBlockIfNeededLocked(ctx, &data, blockNum, blockLabel) {
		// Hydration failed but we still have a valid block number
		// Buffer the data anyway for gap bypass to work
		if blockNum != nil && blockNum.Cmp(s.expectedBlock) > 0 {
			s.buffer[blockNum.String()] = data
			s.enforceBufferLimitLocked()
		}
		return nil
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
	s.enforceBufferLimitLocked()
	return nil
}

func (s *Sequencer) resolveBlockNum(data BlockData) *big.Int {
	if data.Number != nil {
		return data.Number
	}
	if data.Block != nil {
		return data.Block.Number()
	}
	return nil
}

func (s *Sequencer) blockLabel(blockNum *big.Int) string {
	if blockNum == nil {
		return "<nil>"
	}
	return blockNum.String()
}

func (s *Sequencer) handleRangeTeleportLocked(ctx context.Context, data BlockData, blockNum *big.Int) bool {
	if blockNum != nil || data.Block != nil || data.RangeEnd == nil || data.Err != nil {
		return false
	}
	if data.RangeEnd.Cmp(s.expectedBlock) < 0 {
		return true
	}
	nextBlock := new(big.Int).Add(data.RangeEnd, big.NewInt(1))
	s.expectedBlock.Set(nextBlock)
	s.lastProgressAt = time.Now()
	Logger.Debug("sequencer_range_teleport",
		slog.String("from", s.expectedBlock.String()),
		slog.String("to", data.RangeEnd.String()))
	s.processBufferContinuationsLocked(ctx)
	return true
}

func (s *Sequencer) retryFetchIfNeededLocked(ctx context.Context, data BlockData, blockNum *big.Int, blockLabel string) BlockData {
	if data.Err == nil {
		return data
	}
	Logger.Warn("sequencer_fetch_error_retrying", slog.String("block", blockLabel))
	if blockNum == nil {
		return data
	}
	if s.processor == nil {
		return data
	}
	rpcClient := s.processor.GetRPCClient()
	if rpcClient == nil {
		return data
	}
	block, err := rpcClient.BlockByNumber(ctx, blockNum)
	if err != nil {
		return data
	}
	q := ethereum.FilterQuery{FromBlock: blockNum, ToBlock: blockNum, Topics: [][]common.Hash{{TransferEventHash}}}
	logs, err := rpcClient.FilterLogs(ctx, q)
	if err != nil {
		return data
	}
	data.Block = block
	data.Logs = logs
	data.Err = nil
	Logger.Info("sequencer_retry_success", slog.String("block", blockNum.String()))
	return data
}

func (s *Sequencer) hydrateBlockIfNeededLocked(ctx context.Context, data *BlockData, blockNum *big.Int, blockLabel string) bool {
	if data.Block != nil {
		return true
	}
	if blockNum == nil {
		return false
	}
	if s.processor == nil {
		return false
	}
	rpcClient := s.processor.GetRPCClient()
	if rpcClient == nil {
		return false
	}
	block, err := rpcClient.BlockByNumber(ctx, blockNum)
	if err != nil {
		Logger.Warn("sequencer_block_refetch_failed",
			slog.String("block", blockLabel),
			slog.String("err", err.Error()))
		return false
	}
	data.Block = block
	return true
}

func (s *Sequencer) enforceBufferLimitLocked() {
	bufferLimit := 1000
	if s.chainID == 31337 {
		bufferLimit = 50000
	}
	if len(s.buffer) <= bufferLimit {
		return
	}
	var minBuffered *big.Int
	for numStr := range s.buffer {
		if n, ok := new(big.Int).SetString(numStr, 10); ok {
			if minBuffered == nil || n.Cmp(minBuffered) < 0 {
				minBuffered = n
			}
		}
	}
	if minBuffered == nil {
		return
	}
	Logger.Warn("🚧 BUFFER_OVERFLOW_SKIP: Jumping expectedBlock to min buffered",
		slog.String("old_expected", s.expectedBlock.String()),
		slog.String("new_expected", minBuffered.String()),
		slog.Int("buffer_size", len(s.buffer)))
	s.expectedBlock.Set(minBuffered)
	s.buffer = make(map[string]BlockData)
	s.lastProgressAt = time.Now()
}
