package engine

import (
	"context"
	"log/slog"
	"time"
)

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
