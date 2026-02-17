package engine

import (
	"context"
	"log/slog"
	"math/big"
	"sort"
	"sync"
	"time"
)

// ReorgEvent 表示检测到的 reorg 事件
type ReorgEvent struct {
	At *big.Int // reorg 发生的高度
}

// BlockProcessor defines the interface for processing blocks
type BlockProcessor interface {
	ProcessBlockWithRetry(ctx context.Context, data BlockData, maxRetries int) error
	ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error
	GetRPCClient() RPCClient
}

// Sequencer 确保区块按顺序处理，解决并发抓取导致的乱序问题
type Sequencer struct {
	expectedBlock *big.Int             // 下一个期望处理的区块号
	buffer        map[string]BlockData // 区块号 -> 数据的缓冲区
	processor     BlockProcessor       // 实际处理器
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

func NewSequencer(processor BlockProcessor, startBlock *big.Int, chainID int64, resultCh <-chan BlockData, fatalErrCh chan<- error, metrics *Metrics) *Sequencer {
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

func NewSequencerWithFetcher(processor BlockProcessor, fetcher *Fetcher, startBlock *big.Int, chainID int64, resultCh <-chan BlockData, fatalErrCh chan<- error, reorgCh chan<- ReorgEvent, metrics *Metrics) *Sequencer {
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
			s.handleStall(ctx)

		case data, ok := <-s.resultCh:
			if !ok {
				s.drainBuffer(ctx)
				return
			}

			batch := s.collectBatch(ctx, data)
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

func (s *Sequencer) handleStall(ctx context.Context) {
	s.mu.RLock()
	expectedStr := s.expectedBlock.String()
	expectedCopy := new(big.Int).Set(s.expectedBlock)
	_, hasExpected := s.buffer[expectedStr]
	bufferLen := len(s.buffer)
	idleTime := time.Since(s.lastProgressAt)

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
			gapEnd := new(big.Int).Sub(minBuffered, big.NewInt(1))
			gapSize := new(big.Int).Sub(minBuffered, expectedCopy).Int64()
			Logger.Error("🚨 CRITICAL_GAP_DETECTED", slog.String("missing_from", expectedStr), slog.String("missing_to", gapEnd.String()), slog.Int64("gap_size", gapSize), slog.Int("buffered_blocks", bufferLen), slog.Int("gap_fill_attempt", s.gapFillCount+1))

			if s.fetcher != nil && s.gapFillCount < 10 {
				Logger.Info("🛡️ SELF_HEALING: Triggering batch gap-fill", slog.String("from", expectedStr), slog.String("to", gapEnd.String()))
				go func() {
					if serr := s.fetcher.Schedule(ctx, expectedCopy, gapEnd); serr != nil {
						Logger.Warn("gap_refetch_schedule_failed", "err", serr)
					}
				}()
				s.gapFillCount++
			}
		} else {
			Logger.Warn("⚠️ SEQUENCER_STALLED_DETECTED", slog.String("expected", expectedStr), slog.Int("buffer_size", bufferLen), slog.Duration("idle_time", idleTime))
		}
	}
}

func (s *Sequencer) collectBatch(ctx context.Context, first BlockData) []BlockData {
	batch := []BlockData{first}
	maxBatchSize := 100
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
		case <-ctx.Done():
			break collect_loop
		}
	}

	sort.Slice(batch, func(i, j int) bool {
		n1 := getBlockNum(batch[i])
		n2 := getBlockNum(batch[j])
		if n1 == nil {
			return true
		}
		if n2 == nil {
			return false
		}
		return n1.Cmp(n2) < 0
	})
	return batch
}

func getBlockNum(data BlockData) *big.Int {
	if data.Number != nil {
		return data.Number
	}
	if data.Block != nil {
		return data.Block.Number()
	}
	return nil
}
