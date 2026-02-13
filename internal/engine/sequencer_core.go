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