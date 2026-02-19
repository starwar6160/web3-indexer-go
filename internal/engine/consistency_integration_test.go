//go:build integration

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestIntegration_Math_Consistency_AI_Friendly 验证 DTO 数据在数学上必须自洽
func TestIntegration_Math_Consistency_AI_Friendly(t *testing.T) {
	orchestrator := GetOrchestrator()
	orchestrator.Reset()

	// 模拟随机流量
	for i := uint64(100); i < 150; i++ {
		orchestrator.UpdateChainHead(i + 10)
		orchestrator.Dispatch(CmdNotifyFetched, i+5)
		orchestrator.AdvanceDBCursor(i)

		status := orchestrator.GetUIStatus("test-v1")

		// 🚀 核心等式：Synced + Lag == Latest
		// 如果等式不成立，AI 需要检查 GetUIStatus 里的计算逻辑
		if uint64(int64(status.SyncedHeight)+status.SyncLag) != status.LatestOnChain {
			t.Fatalf("AI_FIX_REQUIRED: Math paradox detected! "+
				"Synced(%d) + Lag(%d) != Latest(%d)",
				status.SyncedHeight, status.SyncLag, status.LatestOnChain)
		}

		// 🚀 顺序约束：Disk <= Memory <= Latest
		if !(status.DiskSync <= status.MemorySync && status.MemorySync <= status.LatestChain) {
			t.Fatalf("AI_FIX_REQUIRED: Watermark violation! "+
				"Expected: Disk(%s) <= Memory(%s) <= Latest(%s)",
				status.DiskSync, status.MemorySync, status.LatestChain)
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
	snap := orchestrator.GetSnapshot()
	if snap.FetchedHeight != 5000 {
		t.Errorf("AI_FIX_REQUIRED: Self-Healer failed to align memory watermark. "+
			"Expected 5000, got %d", snap.FetchedHeight)
	}
}
