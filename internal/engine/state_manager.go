package engine

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// IndexerState 索引器状态枚举
type IndexerState int32

const (
	StateIdle     IndexerState = iota // 休眠状态
	StateActive                       // 活跃演示状态
	StateWatching                     // 低成本监听状态
)

func (s IndexerState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateActive:
		return "active"
	case StateWatching:
		return "watching"
	default:
		return "unknown"
	}
}

// StateManager 智能状态管理器
type StateManager struct {
	currentState atomic.Int32 // 当前状态
	lastAccess   atomic.Int64 // 最后访问时间(Unix纳秒)

	// 组件引用
	indexer IndexerService
	rpcPool *RPCClientPool

	// 控制通道
	stateCh chan IndexerState
	stopCh  chan struct{}

	// 配置
	demoDuration  time.Duration // 演示模式持续时间
	idleTimeout   time.Duration // 闲置超时时间
	checkInterval time.Duration // 检查间隔

	mu sync.RWMutex
}

// IndexerService 索引器服务接口
type IndexerService interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
	GetCurrentBlock() string
}

// NewStateManager 创建状态管理器
func NewStateManager(indexer IndexerService, rpcPool *RPCClientPool) *StateManager {
	sm := &StateManager{
		indexer:       indexer,
		rpcPool:       rpcPool,
		stateCh:       make(chan IndexerState, 10),
		stopCh:        make(chan struct{}),
		demoDuration:  5 * time.Minute,  // 5分钟演示
		idleTimeout:   10 * time.Minute, // 10分钟无访问自动休眠
		checkInterval: 1 * time.Minute,  // 每分钟检查一次
	}

	// 初始状态为闲置
	sm.currentState.Store(int32(StateIdle))
	sm.lastAccess.Store(time.Now().UnixNano())

	return sm
}

// Start 启动状态管理器
func (sm *StateManager) Start(ctx context.Context) {
	Logger.Info("state_manager_started",
		slog.String("initial_state", sm.GetState().String()),
		slog.Duration("demo_duration", sm.demoDuration),
		slog.Duration("idle_timeout", sm.idleTimeout),
	)

	// 启动看门狗
	go sm.watchdog(ctx)

	// 启动状态处理器
	go sm.stateProcessor(ctx)
}

// watchdog 看门狗 - 监控访问并自动状态切换
func (sm *StateManager) watchdog(ctx context.Context) {
	ticker := time.NewTicker(sm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case <-ticker.C:
			sm.checkAndTransition()
		}
	}
}

// checkAndTransition 检查并执行状态转换
func (sm *StateManager) checkAndTransition() {
	now := time.Now()
	lastAccessNano := sm.lastAccess.Load()
	lastAccess := time.Unix(0, lastAccessNano)
	timeSinceAccess := now.Sub(lastAccess)

	currentState := IndexerState(sm.currentState.Load())

	Logger.Debug("watchdog_check",
		slog.String("current_state", currentState.String()),
		slog.Duration("time_since_access", timeSinceAccess),
	)

	switch currentState {
	case StateActive:
		// 演示模式：检查是否超时
		if timeSinceAccess > sm.demoDuration {
			Logger.Info("demo_timeout_transitioning_to_idle")
			sm.transitionTo(StateIdle)
		}

	case StateIdle:
		// 闲置模式：检查是否需要进入低成本监听
		if timeSinceAccess > sm.idleTimeout {
			Logger.Info("idle_timeout_transitioning_to_watching")
			sm.transitionTo(StateWatching)
		}

	case StateWatching:
		// 监听模式：持续监听，等待新访问
		// 这个状态下几乎不消耗HTTP配额
	}
}

// stateProcessor 状态处理器 - 执行实际的状态转换
func (sm *StateManager) stateProcessor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case newState := <-sm.stateCh:
			sm.executeTransition(ctx, newState)
		}
	}
}

// transitionTo 触发状态转换
func (sm *StateManager) transitionTo(newState IndexerState) {
	select {
	case sm.stateCh <- newState:
	default:
		Logger.Warn("state_channel_full",
			slog.String("new_state", newState.String()),
		)
	}
}

// executeTransition 执行状态转换
func (sm *StateManager) executeTransition(ctx context.Context, newState IndexerState) {
	oldState := IndexerState(sm.currentState.Load())

	if oldState == newState {
		return // 无需转换
	}

	Logger.Info("state_transition",
		slog.String("from", oldState.String()),
		slog.String("to", newState.String()),
	)

	// 执行退出操作
	switch oldState {
	case StateActive:
		sm.stopActiveMode()
	case StateWatching:
		sm.stopWatchingMode()
	}

	// 执行进入操作
	switch newState {
	case StateActive:
		sm.startActiveMode(ctx)
	case StateIdle:
		// 闲置模式不需要特殊操作
	case StateWatching:
		sm.startWatchingMode(ctx)
	}

	// 更新状态
	sm.currentState.Store(int32(newState))

	// 记录指标（暂时注释掉，避免编译错误）
	// GetMetrics().RecordStateTransition(oldState.String(), newState.String())
}

// startActiveMode 启动活跃演示模式
func (sm *StateManager) startActiveMode(ctx context.Context) {
	Logger.Info("starting_active_demo_mode")

	if err := sm.indexer.Start(ctx); err != nil {
		Logger.Error("failed_to_start_indexer",
			slog.String("error", err.Error()),
		)
		return
	}

	Logger.Info("active_demo_mode_started",
		slog.Duration("will_run_for", sm.demoDuration),
	)
}

// stopActiveMode 停止活跃模式
func (sm *StateManager) stopActiveMode() {
	Logger.Info("stopping_active_demo_mode")

	if err := sm.indexer.Stop(); err != nil {
		Logger.Error("failed_to_stop_indexer",
			slog.String("error", err.Error()),
		)
	}

	Logger.Info("active_demo_mode_stopped")
}

// startWatchingMode 启动低成本监听模式
func (sm *StateManager) startWatchingMode(ctx context.Context) {
	Logger.Info("starting_watching_mode",
		slog.String("mode", "low_cost_wss_subscription"),
	)

	// TODO: 实现WSS订阅逻辑
	// 这里只保持最基本的连接，不进行大量数据获取

	Logger.Info("watching_mode_started",
		slog.String("benefit", "minimal_rpc_quota_consumption"),
	)
}

// stopWatchingMode 停止监听模式
func (sm *StateManager) stopWatchingMode() {
	Logger.Info("stopping_watching_mode")
	// TODO: 关闭WSS连接
}

// RecordAccess 记录访问时间（API调用时调用）
func (sm *StateManager) RecordAccess() {
	sm.lastAccess.Store(time.Now().UnixNano())

	// 如果当前是闲置或监听状态，可以触发启动演示模式
	currentState := IndexerState(sm.currentState.Load())
	if currentState == StateIdle || currentState == StateWatching {
		Logger.Info("access_detected_starting_demo_mode")
		sm.transitionTo(StateActive)
	}
}

// StartDemo 手动启动演示模式
func (sm *StateManager) StartDemo() {
	Logger.Info("manual_demo_start_requested")
	sm.RecordAccess() // 更新访问时间
}

// GetState 获取当前状态
func (sm *StateManager) GetState() IndexerState {
	return IndexerState(sm.currentState.Load())
}

// GetStatus 获取详细状态信息
func (sm *StateManager) GetStatus() map[string]interface{} {
	currentState := sm.GetState()
	lastAccessNano := sm.lastAccess.Load()
	lastAccess := time.Unix(0, lastAccessNano)

	status := map[string]interface{}{
		"state":             currentState.String(),
		"last_access":       lastAccess.Format(time.RFC3339),
		"time_since_access": time.Since(lastAccess).String(),
		"indexer_running":   sm.indexer.IsRunning(),
	}

	if sm.indexer.IsRunning() {
		status["current_block"] = sm.indexer.GetCurrentBlock()
	}

	return status
}

// Stop 停止状态管理器
func (sm *StateManager) Stop() {
	Logger.Info("stopping_state_manager")
	close(sm.stopCh)

	// 停止索引器
	if sm.indexer.IsRunning() {
		sm.indexer.Stop()
	}
}
