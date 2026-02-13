package engine

import (
	"context"
	"log/slog"
	"math/big"
	"time"
)

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