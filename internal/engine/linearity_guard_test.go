package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// createMockOrchestrator 创建一个用于测试的 mock Orchestrator
func createMockOrchestrator(synced, fetched, latest uint64) *Orchestrator {
	return &Orchestrator{
		cmdChan: make(chan Message, 100), // 🔥 初始化 channel 防止 Dispatch 阻塞
		state: CoordinatorState{
			SyncedCursor:  synced,
			FetchedHeight: fetched,
			LatestHeight:  latest,
			UpdatedAt:     time.Now(),
		},
		snapshot: CoordinatorState{
			SyncedCursor:  synced,
			FetchedHeight: fetched,
			LatestHeight:  latest,
			UpdatedAt:     time.Now(),
		},
	}
}

// TestIntegration_Chain_Regression_AI_Friendly 验证链回退/重置场景下的状态坍缩
// 这是博彩级系统中最危险的状态之一：索引器领先于区块链（"位点倒挂"）
func TestIntegration_Chain_Regression_AI_Friendly(t *testing.T) {
	// 使用独立的 LinearityGuard 进行测试（避免单例并发问题）
	mockOrch := createMockOrchestrator(40000, 40000, 40000)
	guard := NewLinearityGuard(mockOrch)

	// 1. 模拟 RPC 突然报告当前只有 8000 块 (Anvil 重置/回退)
	rpcHeight := uint64(8000)

	// 2. 触发 CheckLinearity
	guard.CheckLinearity(rpcHeight)

	// 3. 验证系统是否已经坍缩到当前链尖
	snap := mockOrch.GetSnapshot()

	// 关键断言：SyncedCursor 应该 <= 8000（坍缩完成）
	assert.LessOrEqual(t, snap.SyncedCursor, rpcHeight,
		"AI_FIX_REQUIRED: Failed to collapse state. Indexer is still in the future!")

	t.Logf("✅ SUCCESS: State collapsed correctly. SyncedCursor: %d -> %d (chain: %d)",
		40000, snap.SyncedCursor, rpcHeight)
}

// TestLinearityGuard_Diagnostic 验证诊断信息输出
func TestLinearityGuard_Diagnostic(t *testing.T) {
	mockOrch := createMockOrchestrator(39601, 39601, 39601)
	guard := NewLinearityGuard(mockOrch)

	// 获取诊断信息
	diag := guard.GetDiagnostic()

	// 验证诊断字段
	assert.NotNil(t, diag["synced_cursor"], "Diagnostic should include synced_cursor")
	assert.NotNil(t, diag["latest_height"], "Diagnostic should include latest_height")
	assert.NotNil(t, diag["is_time_paradox"], "Diagnostic should include is_time_paradox")

	t.Logf("✅ SUCCESS: LinearityGuard diagnostic working. Paradox detected: %v", diag["is_time_paradox"])
}

// TestLinearityGuard_SmallRegression 验证小幅回退不会触发坍缩
func TestLinearityGuard_SmallRegression(t *testing.T) {
	mockOrch := createMockOrchestrator(1000, 1000, 1000)
	guard := NewLinearityGuard(mockOrch)

	// 小幅回退 50 个块（小于 collapseThreshold）
	rpcHeight := uint64(950)
	guard.CheckLinearity(rpcHeight)

	snap := mockOrch.GetSnapshot()
	// 小幅回退不应触发坍缩（需要3次连续才会触发）
	assert.Equal(t, uint64(1000), snap.SyncedCursor,
		"Small regression should not trigger collapse immediately")

	t.Logf("✅ SUCCESS: Small regression handled correctly (50 blocks)")
}

// TestLinearityGuard_LargeRegression 验证大幅回退触发坍缩
func TestLinearityGuard_LargeRegression(t *testing.T) {
	mockOrch := createMockOrchestrator(40000, 40000, 40000)
	guard := NewLinearityGuard(mockOrch)

	// 大幅回退到 8000（超过 collapseThreshold=100）
	rpcHeight := uint64(8000)
	guard.CheckLinearity(rpcHeight)

	snap := mockOrch.GetSnapshot()
	// 大幅回退应该触发坍缩
	assert.LessOrEqual(t, snap.SyncedCursor, rpcHeight,
		"Large regression should trigger state collapse")

	t.Logf("✅ SUCCESS: Large regression triggered state collapse (40000 -> %d)", snap.SyncedCursor)
}
