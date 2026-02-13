package engine

import (
	"context"
	"log/slog"
)

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
		// 闲置模式：不仅是停顿，还要确保资源释放
		Logger.Info("entering_true_sleep_mode_releasing_resources")
		sm.indexer.Stop()
	case StateWatching:
		sm.startWatchingMode(ctx)
	}

	// 更新状态
	sm.currentState.Store(int32(newState))
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
		slog.String("mode", "low_cost_header_sync"),
	)

	// 如果索引器未运行，先启动它
	if !sm.indexer.IsRunning() {
		if err := sm.indexer.Start(ctx); err != nil {
			Logger.Error("failed_to_start_indexer_for_watching", slog.String("error", err.Error()))
			return
		}
	}

	// 开启低功耗模式（仅同步区块头，不抓取Logs）
	sm.indexer.SetLowPowerMode(true)

	Logger.Info("watching_mode_started",
		slog.String("benefit", "minimal_rpc_quota_consumption"),
	)
}

// stopWatchingMode 停止监听模式
func (sm *StateManager) stopWatchingMode() {
	Logger.Info("stopping_watching_mode")
	// 恢复全量数据抓取模式
	sm.indexer.SetLowPowerMode(false)
}