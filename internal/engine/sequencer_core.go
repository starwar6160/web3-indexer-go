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
	Logger.Info("🚀 Sequencer started", "expected_block", s.expectedBlock.String())

	stallTicker := time.NewTicker(30 * time.Second)
	defer stallTicker.Stop()

	processedCount := 0
	pulseTicker := time.NewTicker(10 * time.Second)
	defer pulseTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.drainBuffer(ctx)
			return

		case <-stallTicker.C:
			s.handleStall(ctx)

		case <-pulseTicker.C:
			slog.Info("🚀 Sequencer: Pulse",
				"expected", s.expectedBlock.String(),
				"buffer", len(s.buffer),
				"processed_since_last", processedCount)
			processedCount = 0

		case data, ok := <-s.resultCh:
			if !ok {
				s.drainBuffer(ctx)
				return
			}

			batch := s.collectBatch(ctx, data)
			processedCount += len(batch)
			if err := s.handleBatch(ctx, batch); err != nil {
				// 🔥 关键修复：使用非阻塞 select 发送错误，防止下游消费者（Supervisor）
				// 处理不及时导致 Sequencer 主循环永久死锁。
				select {
				case s.fatalErrCh <- err:
					// 成功上报
				default:
					slog.Error("⚠️ Sequencer fatalErrCh full, dropping error report to avoid deadlock",
						"err", err.Error(),
						"expected", s.expectedBlock.String(),
						"buffer", len(s.buffer))
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

	if idleTime > 55*time.Second {
		if minBuffered != nil && minBuffered.Cmp(expectedCopy) > 0 {
			gapEnd := new(big.Int).Sub(minBuffered, big.NewInt(1))
			gapSize := new(big.Int).Sub(minBuffered, expectedCopy).Int64()
			Logger.Error("🚨 CRITICAL_GAP_DETECTED",
				slog.String("missing_from", expectedStr),
				slog.String("missing_to", gapEnd.String()),
				slog.Int64("gap_size", gapSize),
				slog.Int("buffered_blocks", bufferLen),
				slog.Int("gap_fill_attempt", s.gapFillCount+1))

			if s.fetcher != nil && s.gapFillCount < 3 {
				Logger.Info("🛡️ SELF_HEALING: Triggering batch gap-fill",
					slog.String("from", expectedStr),
					slog.String("to", gapEnd.String()),
					slog.Int("attempt", s.gapFillCount+1))
				go func(gapCtx context.Context) {
					if serr := s.fetcher.Schedule(gapCtx, expectedCopy, gapEnd); serr != nil {
						Logger.Warn("gap_refetch_schedule_failed", "err", serr)
					}
				}(ctx)
				s.gapFillCount++
			} else {
				skippedTo := new(big.Int).Sub(minBuffered, big.NewInt(1))
				Logger.Error("🚧 GAP_BYPASS: Forced skip after max retries",
					slog.String("skipped_from", expectedStr),
					slog.String("skipped_to", skippedTo.String()),
					slog.String("resume_at", minBuffered.String()),
					slog.Int("gap_fill_attempts", s.gapFillCount),
					slog.Int64("gap_size", gapSize))

				// 🔥 FINDING-7: 通过 Orchestrator 持久化 gap 审计记录
				GetOrchestrator().DispatchLog("AUDIT", "GAP_BYPASS",
					"skipped_from", expectedStr,
					"skipped_to", skippedTo.String(),
					"resume_at", minBuffered.String(),
					"gap_size", gapSize,
					"gap_fill_attempts", s.gapFillCount)
				if s.metrics != nil && s.metrics.SelfHealingTriggered != nil {
					s.metrics.SelfHealingTriggered.Inc()
				}

				s.lastProgressAt = time.Now()
				s.mu.Lock()
				s.expectedBlock.Set(minBuffered)
				s.gapFillCount = 0
				s.mu.Unlock()
			}
		} else {
			Logger.Error("🚨 CRITICAL_STALL: Processor blocked, forcing skip",
				slog.String("stuck_at", expectedStr),
				slog.Duration("idle_time", idleTime),
				slog.Int("buffer_size", bufferLen))

			s.lastProgressAt = time.Now()
			s.mu.Lock()
			s.expectedBlock.Add(s.expectedBlock, big.NewInt(1))
			s.gapFillCount = 0
			s.mu.Unlock()
		}
	} else if idleTime > 30*time.Second {
		Logger.Warn("⚠️ SEQUENCER_STALLED_DETECTED",
			slog.String("expected", expectedStr),
			slog.Int("buffer_size", bufferLen),
			slog.Duration("idle_time", idleTime))
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

	// 🚀 优化排序性能：预处理提取所有 block number，避免在比较函数中重复调用 getBlockNum
	type sortableBlock struct {
		idx int
		num *big.Int
	}
	nums := make([]sortableBlock, len(batch))
	for i, data := range batch {
		nums[i] = sortableBlock{idx: i, num: getBlockNum(data)}
	}

	sort.Slice(nums, func(i, j int) bool {
		if nums[i].num == nil {
			return true
		}
		if nums[j].num == nil {
			return false
		}
		return nums[i].num.Cmp(nums[j].num) < 0
	})

	// 根据排序后的索引重建切片
	sorted := make([]BlockData, len(batch))
	for i, nb := range nums {
		sorted[i] = batch[nb.idx]
	}
	return sorted
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

// GetIdleTime 返回 Sequencer 的闲置时间（只读，用于看门狗检测）
func (s *Sequencer) GetIdleTime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.lastProgressAt)
}

// GetExpectedBlock 返回当前期望的区块号（只读，用于看门狗检测）
func (s *Sequencer) GetExpectedBlock() *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return new(big.Int).Set(s.expectedBlock)
}

// ResetExpectedBlock 强制重置期望区块（看门狗专用）
// 同时重置闲置计时器，避免立即再次触发看门狗
func (s *Sequencer) ResetExpectedBlock(block *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expectedBlock.Set(block)
	s.lastProgressAt = time.Now() // 重置闲置计时器
	Logger.Debug("🛡️ Sequencer: Expected block reset by watchdog",
		slog.String("new_expected", block.String()))
}

// ClearBuffer 清空缓冲区（看门狗专用）
func (s *Sequencer) ClearBuffer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldSize := len(s.buffer)
	s.buffer = make(map[string]BlockData)
	Logger.Debug("🛡️ Sequencer: Buffer cleared by watchdog",
		slog.Int("old_size", oldSize))
}
