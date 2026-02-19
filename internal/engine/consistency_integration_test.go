//go:build integration

package engine

import (
	"context"
	"testing"
	"time"
)

// TestIntegration_Math_Consistency_AI_Friendly 验证 DTO 数据在数学上必须自洽
func TestIntegration_Math_Consistency_AI_Friendly(t *testing.T) {
	orchestrator := GetOrchestrator()
	orchestrator.Reset()

	// 模拟随机流量
	for i := uint64(100); i < 150; i++ {
		orchestrator.UpdateChainHead(i + 10)
		orchestrator.Dispatch(CmdNotifyFetchProgress, i+5)
		orchestrator.AdvanceDBCursor(i)

		// 🚀 核心：给予充足时间让 process 循环更新 snapshot
		time.Sleep(100 * time.Millisecond)

		// 获取一个临时 DB 引用 (此处可以传 nil 因为测试不需要真正的数据库计算，或者使用 mock)
		status := orchestrator.GetUIStatus(context.Background(), nil, "test-v1")
		snap := orchestrator.GetSnapshot()

		// 🚀 核心等式：Synced + Lag == Latest
		// #nosec G115 - SyncedCursor realistically fits in int64
		if uint64(int64(snap.SyncedCursor)+status.SyncLag) != snap.LatestHeight {
			t.Fatalf("AI_FIX_REQUIRED: Math paradox detected! "+
				"Synced(%d) + Lag(%d) != Latest(%d)",
				snap.SyncedCursor, status.SyncLag, snap.LatestHeight)
		}

		// 🚀 顺序约束：Disk <= Memory <= Latest
		if !(snap.SyncedCursor <= snap.FetchedHeight && snap.FetchedHeight <= snap.LatestHeight) {
			t.Fatalf("AI_FIX_REQUIRED: Watermark violation! "+
				"Expected: Disk(%d) <= Memory(%d) <= Latest(%d)",
				snap.SyncedCursor, snap.FetchedHeight, snap.LatestHeight)
		}
	}
}

// TestIntegration_SelfHealing_AI_Friendly 验证自愈审计逻辑
func TestIntegration_SelfHealing_AI_Friendly(t *testing.T) {
	orchestrator := GetOrchestrator()
	orchestrator.Reset()
	healer := NewSelfHealer(orchestrator)

	// 1. 人为注入一个非法的倒挂状态
	orchestrator.mu.Lock()
	orchestrator.state.SyncedCursor = 5000
	orchestrator.state.FetchedHeight = 4000 // 🚀 内存落后于磁盘
	orchestrator.mu.Unlock()

	// 2. 触发自愈
	healer.auditAndHeal()

	// 3. 验证结果
	orchestrator.Dispatch(CmdNotifyFetchProgress, uint64(5000))
	time.Sleep(100 * time.Millisecond)

	snap := orchestrator.GetSnapshot()
	if snap.FetchedHeight != 5000 {
		t.Errorf("AI_FIX_REQUIRED: Self-Healer failed to align memory watermark. "+
			"Expected 5000, got %d", snap.FetchedHeight)
	}
}
