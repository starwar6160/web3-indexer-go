package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

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