package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"runtime"
	"sync"
	"time"
)

// 🔥 动态背压管理器 (Dynamic Backpressure Manager)
// 针对 128G RAM 环境优化，自动调整水位线和调度策略

type BackpressureManager struct {
	// 配置
	maxJobsCapacity    int
	maxResultsCapacity int
	maxSeqBuffer       int

	// 动态水位线（自适应）
	jobsWatermark      int
	resultsWatermark   int
	seqBufferWatermark int

	// 指数退避状态
	lastBlockedTime time.Time
	backoffLevel    int
	backoffMu       sync.Mutex

	// 统计
	totalBlockedCount uint64
}

// NewBackpressureManager 创建背压管理器
func NewBackpressureManager() *BackpressureManager {
	// 🔥 横滨实验室：根据内存大小动态计算容量
	// 假设系统内存 128G，我们可以激进地分配缓冲区
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 每 GB 内存分配 1000 个缓冲区槽位
	memGB := memStats.Sys / (1024 * 1024 * 1024)
	maxResultsCapacity := int(memGB * 1000) // 128G → 128,000

	// 保守一点，设置上限为 100,000
	if maxResultsCapacity > 100000 {
		maxResultsCapacity = 100000
	}
	if maxResultsCapacity < 5000 {
		maxResultsCapacity = 5000 // 最小值
	}

	maxJobsCapacity := maxResultsCapacity / 5 // Jobs 队列小一些
	maxSeqBuffer := 10000                     // Sequencer buffer

	return &BackpressureManager{
		maxJobsCapacity:    maxJobsCapacity,
		maxResultsCapacity: maxResultsCapacity,
		maxSeqBuffer:       maxSeqBuffer,
		jobsWatermark:      maxJobsCapacity * 80 / 100,
		resultsWatermark:   maxResultsCapacity * 80 / 100,
		seqBufferWatermark: 800, // 固定 800 阈值
		backoffLevel:       0,
	}
}

// CheckSchedule 检查是否可以调度（带指数退避）
func (bpm *BackpressureManager) CheckSchedule(jobsDepth, resultsDepth, seqBufferSize int) error {
	// 🔥 1. 基础水位线检查
	if jobsDepth > bpm.jobsWatermark {
		return bpm.blockWithError("jobs", jobsDepth, bpm.maxJobsCapacity, bpm.jobsWatermark, resultsDepth)
	}

	if resultsDepth > bpm.resultsWatermark {
		return bpm.blockWithError("results", resultsDepth, bpm.maxResultsCapacity, bpm.resultsWatermark, jobsDepth)
	}

	if seqBufferSize > bpm.seqBufferWatermark {
		return bpm.blockWithError("sequencer", seqBufferSize, bpm.maxSeqBuffer, bpm.seqBufferWatermark, resultsDepth)
	}

	// 🔥 2. 指数退避检查（防止疯狂重试）
	bpm.backoffMu.Lock()
	defer bpm.backoffMu.Unlock()

	if bpm.backoffLevel > 0 {
		elapsed := time.Since(bpm.lastBlockedTime)
		backoffDuration := time.Duration(1<<bpm.backoffLevel) * 100 * time.Millisecond

		if elapsed < backoffDuration {
			return fmt.Errorf("backoff: waiting %v (level %d)", backoffDuration-elapsed, bpm.backoffLevel)
		}

		// 退避时间到了，重置退避级别
		bpm.backoffLevel = 0
		slog.Info("✅ [Backpressure] Backoff reset", "level", 0)
	}

	return nil
}

// blockWithError 记录阻塞并触发指数退避
func (bpm *BackpressureManager) blockWithError(component string, depth, capacity, watermark, otherDepth int) error {
	bpm.backoffMu.Lock()
	defer bpm.backoffMu.Unlock()

	bpm.totalBlockedCount++
	bpm.lastBlockedTime = time.Now()

	// 增加退避级别（最大 5 级：100ms → 3.2s）
	if bpm.backoffLevel < 5 {
		bpm.backoffLevel++
	}

	backoffDuration := time.Duration(1<<bpm.backoffLevel) * 100 * time.Millisecond

	slog.Warn("🚫 [Backpressure] SCHEDULE_BLOCKED",
		slog.String("component", component),
		slog.Int("depth", depth),
		slog.Int("watermark", watermark),
		slog.Int("capacity", capacity),
		slog.Int("other_depth", otherDepth),
		slog.Int("backoff_level", bpm.backoffLevel),
		slog.Duration("backoff_duration", backoffDuration),
		slog.Uint64("total_blocked", bpm.totalBlockedCount))

	return fmt.Errorf("%s queue backpressure: depth=%d/%d (backoff level %d)", component, depth, capacity, bpm.backoffLevel)
}

// ResetBackoff 重置退避级别（成功调度后调用）
func (bpm *BackpressureManager) ResetBackoff() {
	bpm.backoffMu.Lock()
	defer bpm.backoffMu.Unlock()

	if bpm.backoffLevel > 0 {
		slog.Info("✅ [Backpressure] Backoff cleared", "previous_level", bpm.backoffLevel)
		bpm.backoffLevel = 0
	}
}

// GetCapacity 获取当前容量配置
func (bpm *BackpressureManager) GetCapacity() (maxJobs, maxResults, maxSeq int) {
	return bpm.maxJobsCapacity, bpm.maxResultsCapacity, bpm.maxSeqBuffer
}

// GetWatermarks 获取当前水位线配置
func (bpm *BackpressureManager) GetWatermarks() (jobs, results, seq int) {
	return bpm.jobsWatermark, bpm.resultsWatermark, bpm.seqBufferWatermark
}

// 🔥 任务合并（Task Merging）：防止重复调度

type MergeKey struct {
	Start string
	End   string
}

type ScheduleMerge struct {
	mu          sync.Mutex
	pending     map[MergeKey]time.Time // 最后调度时间
	mergeWindow time.Duration          // 合并窗口（同一范围内的调度会被合并）
}

func NewScheduleMerge(mergeWindow time.Duration) *ScheduleMerge {
	return &ScheduleMerge{
		pending:     make(map[MergeKey]time.Time),
		mergeWindow: mergeWindow,
	}
}

// ShouldSchedule 检查是否应该调度（带合并逻辑）
func (sm *ScheduleMerge) ShouldSchedule(start, end *big.Int) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := MergeKey{
		Start: start.String(),
		End:   end.String(),
	}

	now := time.Now()
	if lastTime, exists := sm.pending[key]; exists {
		// 如果在合并窗口内，跳过重复调度
		if now.Sub(lastTime) < sm.mergeWindow {
			slog.Debug("🔄 [Merge] Schedule skipped (merged)",
				slog.String("range", fmt.Sprintf("%s-%s", start.String(), end.String())),
				slog.Duration("since_last", now.Sub(lastTime)))
			return false
		}
	}

	// 更新最后调度时间
	sm.pending[key] = now
	return true
}

// Cleanup 清理过期的合并记录
func (sm *ScheduleMerge) Cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for key, lastTime := range sm.pending {
		if now.Sub(lastTime) > 10*time.Minute {
			delete(sm.pending, key)
		}
	}
}

// 🔥 Fetcher 集成：使用动态背压管理器

// FetcherWithDynamicBackpressure 支持动态背压的 Fetcher
type FetcherWithDynamicBackpressure struct {
	*Fetcher

	backpressureMgr *BackpressureManager
	scheduleMerge   *ScheduleMerge
}

func NewFetcherWithDynamicBackpressure(fetcher *Fetcher) *FetcherWithDynamicBackpressure {
	return &FetcherWithDynamicBackpressure{
		Fetcher:         fetcher,
		backpressureMgr: NewBackpressureManager(),
		scheduleMerge:   NewScheduleMerge(5 * time.Second), // 5秒合并窗口
	}
}

// ScheduleDynamic 使用动态背压检测的调度方法
func (f *FetcherWithDynamicBackpressure) ScheduleDynamic(ctx context.Context, start, end *big.Int) error {
	// 1. 任务合并检查
	if !f.scheduleMerge.ShouldSchedule(start, end) {
		return nil // 已合并，跳过
	}

	// 2. 动态背压检测
	jobsDepth := len(f.jobs)
	resultsDepth := len(f.Results)
	seqBufferSize := 0
	if f.sequencer != nil {
		seqBufferSize = f.sequencer.GetBufferSize()
	}

	if err := f.backpressureMgr.CheckSchedule(jobsDepth, resultsDepth, seqBufferSize); err != nil {
		return err
	}

	// 3. 执行调度
	if err := f.Schedule(ctx, start, end); err != nil {
		return err
	}

	// 4. 成功调度后重置退避
	f.backpressureMgr.ResetBackoff()

	return nil
}

// GetBackpressureStats 获取背压统计信息
func (f *FetcherWithDynamicBackpressure) GetBackpressureStats() map[string]interface{} {
	maxJobs, maxResults, maxSeq := f.backpressureMgr.GetCapacity()
	jobsWm, resultsWm, seqWm := f.backpressureMgr.GetWatermarks()

	return map[string]interface{}{
		"max_jobs_capacity":     maxJobs,
		"max_results_capacity":  maxResults,
		"max_seq_buffer":        maxSeq,
		"jobs_watermark":        jobsWm,
		"results_watermark":     resultsWm,
		"seq_watermark":         seqWm,
		"current_jobs_depth":    len(f.jobs),
		"current_results_depth": len(f.Results),
		"current_seq_buffer":    f.sequencer.GetBufferSize(),
		"total_blocked_count":   f.backpressureMgr.totalBlockedCount,
	}
}
