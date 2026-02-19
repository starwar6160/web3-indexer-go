package engine

import (
	"log/slog"
	"sync"
	"time"
)

// SystemStateEnum 系统状态枚举
type SystemStateEnum int

const (
	SystemStateUnknown SystemStateEnum = iota
	SystemStateIdle
	SystemStateActive
	SystemStateCatchingUp
	SystemStateStalled
	SystemStateHealing
	SystemStateDegraded   // 🚀 压力过大，正在减压
	SystemStateRunning    // 🚀 正常运行
	SystemStateOptimizing // 🚀 性能调优中
	SystemStateThrottled  // 🚀 背压限流中
)

const (
	systemStateUnknownStr = "unknown"
	systemStateStalledStr = "stalled"
)

func (s SystemStateEnum) String() string {
	switch s {
	case SystemStateIdle:
		return "idle"
	case SystemStateActive:
		return "active"
	case SystemStateCatchingUp:
		return "catching_up"
	case SystemStateStalled:
		return systemStateStalledStr
	case SystemStateHealing:
		return "healing"
	case SystemStateDegraded:
		return "degraded"
	case SystemStateRunning:
		return "running"
	case SystemStateOptimizing:
		return "optimizing"
	case SystemStateThrottled:
		return "throttled"
	default:
		return systemStateUnknownStr
	}
}

// Snapshot 全局状态快照（线程安全）
type Snapshot struct {
	OnChainHeight  uint64          // 链上最新高度
	DBCursor       uint64          // 数据库游标（已索引）
	TotalTransfers uint64          // 总转账数
	SystemState    SystemStateEnum // 系统状态
	PipelineDepth  int32           // 流水线深度（队列深度）
	QuotaUsage     float64         // 配额使用率
	LastUpdate     time.Time       // 快照时间戳

	// 🔥 横滨实验室：背压检测
	JobsQueueDepth  int32 // Fetcher jobs 队列深度
	ResultsDepth    int32 // Fetcher results 队列深度
	SequencerBuffer int32 // Sequencer buffer 深度
}

// GlobalState 全局状态单例（SSOT）
// 所有子系统（StateManager, LazyManager, Watchdog, Metrics）只能通过此单例更新状态
type GlobalState struct {
	mu             sync.RWMutex
	onChainHeight  uint64
	dbCursor       uint64
	totalTransfers uint64
	systemState    SystemStateEnum
	pipelineDepth  int32
	quotaUsage     float64
	lastUpdate     time.Time

	// 🔥 背压检测配置
	maxJobsCapacity    int32
	maxResultsCapacity int32
	maxSequencerBuffer int32

	// 🔥 背压检测实时数据
	jobsQueueDepth  int32
	resultsDepth    int32
	sequencerBuffer int32

	// 订阅者
	subscribers []chan Snapshot
}

var (
	globalState     *GlobalState
	globalStateOnce sync.Once
)

// GetGlobalState 返回全局状态单例
func GetGlobalState() *GlobalState {
	globalStateOnce.Do(func() {
		globalState = &GlobalState{
			systemState:        SystemStateUnknown,
			lastUpdate:         time.Now(),
			subscribers:        make([]chan Snapshot, 0, 8), // 最多 8 个订阅者
			maxJobsCapacity:    200,                         // cap(f.jobs)
			maxResultsCapacity: 5000,
			maxSequencerBuffer: 1000, // 默认 buffer 上限
			jobsQueueDepth:     0,
			resultsDepth:       0,
			sequencerBuffer:    0,
		}
		slog.Info("🌐 GlobalState SSOT initialized")
	})
	return globalState
}

// Snapshot 获取当前状态快照（读锁）
func (s *GlobalState) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Snapshot{
		OnChainHeight:   s.onChainHeight,
		DBCursor:        s.dbCursor,
		TotalTransfers:  s.totalTransfers,
		SystemState:     s.systemState,
		PipelineDepth:   s.pipelineDepth,
		QuotaUsage:      s.quotaUsage,
		LastUpdate:      s.lastUpdate,
		JobsQueueDepth:  s.jobsQueueDepth,
		ResultsDepth:    s.resultsDepth,
		SequencerBuffer: s.sequencerBuffer,
	}
}

// 🔥 状态更新方法（写锁）- 所有子系统必须通过这些方法更新状态

// UpdateChainHead 更新链头高度（由 TailFollow 调用）
func (s *GlobalState) UpdateChainHead(height uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if height > s.onChainHeight {
		s.onChainHeight = height
		s.lastUpdate = time.Now()
		s.notifySubscribers()
	}
}

// AdvanceDBCursor 前进数据库游标（由 Processor 调用）
func (s *GlobalState) AdvanceDBCursor(newCursor uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newCursor > s.dbCursor {
		s.dbCursor = newCursor
		s.lastUpdate = time.Now()
		s.notifySubscribers()
	}
}

// IncrementTransfers 增加转账计数（由 Processor 调用）
func (s *GlobalState) IncrementTransfers(count uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalTransfers += count
	s.lastUpdate = time.Now()
}

// SetSystemState 设置系统状态（由 StateManager 调用）
func (s *GlobalState) SetSystemState(state SystemStateEnum) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.systemState != state {
		oldState := s.systemState
		s.systemState = state
		s.lastUpdate = time.Now()
		s.notifySubscribers()

		slog.Info("🔄 [GlobalState] System state changed",
			"old", oldState.String(),
			"new", state.String())
	}
}

// UpdatePipelineDepth 更新流水线深度（由背压检测器调用）
func (s *GlobalState) UpdatePipelineDepth(jobsDepth, resultsDepth, sequencerBuffer int32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobsQueueDepth = jobsDepth
	s.resultsDepth = resultsDepth
	s.sequencerBuffer = sequencerBuffer
	s.lastUpdate = time.Now()

	// 计算总流水线深度
	s.pipelineDepth = jobsDepth + resultsDepth + sequencerBuffer
}

// RecordQuota 记录配额使用（由限流器调用）
func (s *GlobalState) RecordQuota(usage float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.quotaUsage = usage
	s.lastUpdate = time.Now()
}

// notifySubscribers 通知所有订阅者
func (s *GlobalState) notifySubscribers() {
	snap := s.Snapshot()
	for _, ch := range s.subscribers {
		select {
		case ch <- snap:
		default:
			// 订阅者太慢，跳过
		}
	}
}

// Subscribe 订阅状态快照（用于 Prometheus exporter）
func (s *GlobalState) Subscribe() <-chan Snapshot {
	ch := make(chan Snapshot, 100) // 缓冲 100 个快照
	s.mu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.mu.Unlock()
	return ch
}

// GetCapacity 获取容量配置（用于背压计算）
func (s *GlobalState) GetCapacity() (maxJobs, maxResults, maxSequencer int32) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxJobsCapacity, s.maxResultsCapacity, s.maxSequencerBuffer
}

// SetCapacity 设置容量配置（由 Fetcher/Sequencer 初始化时调用）
func (s *GlobalState) SetCapacity(maxJobs, maxResults, maxSequencer int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxJobsCapacity = maxJobs
	s.maxResultsCapacity = maxResults
	s.maxSequencerBuffer = maxSequencer
}

// 🔥 Helper methods for debugging

// GetStateString 获取状态字符串（用于日志）
func (s *GlobalState) GetStateString() string {
	snap := s.Snapshot()
	return snap.SystemState.String()
}

// GetSyncLag 获取同步滞后
func (s *GlobalState) GetSyncLag() int64 {
	snap := s.Snapshot()
	if snap.OnChainHeight == 0 {
		return 0
	}
	return SafeInt64Diff(snap.OnChainHeight, snap.DBCursor)
}

// IsStalled 检查是否停滞
func (s *GlobalState) IsStalled() bool {
	snap := s.Snapshot()
	return snap.SystemState == SystemStateStalled
}
