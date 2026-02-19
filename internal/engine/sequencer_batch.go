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
	blockNum := data.Number
	if blockNum == nil && data.Block != nil {
		blockNum = data.Block.Number()
	}
	blockLabel := "<nil>"
	if blockNum != nil {
		blockLabel = blockNum.String()
	}

	// Handle pure range progress signal only when there is no concrete block number.
	// Normal empty-block items still carry Number and must not teleport expectedBlock.
	if blockNum == nil && data.Block == nil && data.RangeEnd != nil && data.Err == nil {
		if data.RangeEnd.Cmp(s.expectedBlock) >= 0 {
			// Teleport progress forward
			nextBlock := new(big.Int).Add(data.RangeEnd, big.NewInt(1))
			s.expectedBlock.Set(nextBlock)
			s.lastProgressAt = time.Now()
			Logger.Debug("sequencer_range_teleport",
				slog.String("from", s.expectedBlock.String()),
				slog.String("to", data.RangeEnd.String()))
			s.processBufferContinuationsLocked(ctx)
		}
		return nil
	}

	if data.Err != nil {
		Logger.Warn("sequencer_fetch_error_retrying", slog.String("block", blockLabel))
		if blockNum != nil {
			var err error
			rpcClient := s.processor.GetRPCClient()
			data.Block, err = rpcClient.BlockByNumber(ctx, blockNum)
			if err == nil {
				q := ethereum.FilterQuery{FromBlock: blockNum, ToBlock: blockNum, Topics: [][]common.Hash{{TransferEventHash}}}
				data.Logs, err = rpcClient.FilterLogs(ctx, q)
				if err == nil {
					data.Err = nil
					Logger.Info("sequencer_retry_success", slog.String("block", blockNum.String()))
				}
			}
		}
		if data.Err != nil {
			// 🚀 🔥 资深修复：不返回错误，仅记录警告并允许继续循环。
			// 这样可以让系统保持运行，依靠 handleStall 进行自愈或在下个批次重试。
			Logger.Warn("⚠️ Sequencer: temporary fetch failure, holding block",
				slog.String("block", blockLabel),
				slog.String("err", data.Err.Error()))
			return nil
		}
	}

	// Empty blocks may arrive without hydrated Block object.
	// Fetch the header lazily so sequential processing can still advance per block.
	if data.Block == nil {
		if blockNum == nil {
			return nil
		}
		rpcClient := s.processor.GetRPCClient()
		block, err := rpcClient.BlockByNumber(ctx, blockNum)
		if err != nil {
			Logger.Warn("sequencer_block_refetch_failed",
				slog.String("block", blockLabel),
				slog.String("err", err.Error()))
			return nil
		}
		data.Block = block
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

	// 🔥 Anvil 环境使用更大的 buffer 限制（利用 16G/128G RAM）
	bufferLimit := 1000
	if s.chainID == 31337 {
		bufferLimit = 50000
	}

	if len(s.buffer) > bufferLimit {
		// 🚀 不崩溃，而是跳过 gap 到最小缓冲块
		var minBuffered *big.Int
		for numStr := range s.buffer {
			if n, ok := new(big.Int).SetString(numStr, 10); ok {
				if minBuffered == nil || n.Cmp(minBuffered) < 0 {
					minBuffered = n
				}
			}
		}
		if minBuffered != nil {
			Logger.Warn("🚧 BUFFER_OVERFLOW_SKIP: Jumping expectedBlock to min buffered",
				slog.String("old_expected", s.expectedBlock.String()),
				slog.String("new_expected", minBuffered.String()),
				slog.Int("buffer_size", len(s.buffer)))
			s.expectedBlock.Set(minBuffered)
			s.buffer = make(map[string]BlockData) // 清空 buffer，重新收集
			s.lastProgressAt = time.Now()
		}
	}
	return nil
}
