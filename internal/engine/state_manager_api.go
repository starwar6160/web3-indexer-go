package engine

import (
	"time"
)

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

// Stop 偌止状态管理器
func (sm *StateManager) Stop() {
	Logger.Info("stopping_state_manager")
	close(sm.stopCh)

	// 偌止索引器
	if sm.indexer.IsRunning() {
		sm.indexer.Stop()
	}
}