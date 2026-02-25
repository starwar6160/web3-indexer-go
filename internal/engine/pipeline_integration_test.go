//go:build integration

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/stretchr/testify/assert"
)

// TestStage1_Ingestion_Pulse 验证摄取层：大脑是否感知到链的高度脉动
func TestStage1_Ingestion_Pulse(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()

	// 模拟监听到新块高度 50000
	o.UpdateChainHead(50000)

	// 允许单例循环处理
	time.Sleep(200 * time.Millisecond)
	snap := o.GetSnapshot()

	if snap.LatestHeight != 50000 {
		t.Fatalf("AI_FIX_REQUIRED [Stage 1]: Ingestion failed. LatestHeight expected 50000, got %d", snap.LatestHeight)
	}
}

// TestStage2_Scheduler_Saturation 验证调度层：背压水位线是否能准确拦截过载
func TestStage2_Scheduler_Saturation(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()

	// 获取一个真实的 Fetcher 实例 (由 ServiceManager 模拟)
	rpcPool, err := NewRPCClientPool([]string{"http://localhost:8545"})
	if err != nil {
		t.Fatalf("Failed to create RPC pool: %v", err)
	}
	f := NewFetcher(rpcPool, 4)

	// 0. 准备：设置足够高的链高以通过边界检查
	o.UpdateChainHead(100000)
	time.Sleep(100 * time.Millisecond)

	// 1. 人为制造背压：填满 Results Channel (Capacity 15000)
	// 我们填到 91% 触发 90% 的 watermark
	target := cap(f.Results) * 91 / 100
	for i := 0; i < target; i++ {
		f.Results <- BlockData{Number: models.NewBigInt(int64(i)).Int}
	}

	// 2. 尝试调度
	err = f.Schedule(context.Background(), models.NewBigInt(60000).Int, models.NewBigInt(60100).Int)

	if err == nil || !strings.Contains(err.Error(), "backpressure") {
		t.Fatalf("AI_FIX_REQUIRED [Stage 2]: Scheduler failed to trigger backpressure. Got err: %v", err)
	}
	t.Logf("✅ Stage 2 passed: Backpressure correctly identified at depth %d", len(f.Results))
}

// TestStage3_ShadowSync_Movement 验证获取层：影子高度是否能在落盘前先行跳动
func TestStage3_ShadowSync_Movement(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()

	// 模拟 Fetcher 成功抓取 Block 50001
	o.Dispatch(CmdNotifyFetchProgress, uint64(50001))

	time.Sleep(200 * time.Millisecond)
	snap := o.GetSnapshot()

	// 影子游标 (MemorySync) 必须反映 50001
	if snap.FetchedHeight != 50001 {
		t.Fatalf("AI_FIX_REQUIRED [Stage 3]: Shadow Sync failed. FetchedHeight expected 50001, got %d", snap.FetchedHeight)
	}
}

// TestStage4_Persistence_Finalization 验证持久层：磁盘游标是否能完成物理确认
func TestStage4_Persistence_Finalization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	o := GetOrchestrator()
	o.Reset()

	// 模拟物理确认
	o.AdvanceDBCursor(66)

	// 虽然 AdvanceDBCursor 直接修改了 state，但 snapshot 更新需要经过 process 循环触发
	// 这里我们发送一个空消息来触发一次 snapshot 刷新
	o.Dispatch(CmdNotifyFetched, uint64(66))

	time.Sleep(300 * time.Millisecond)
	status := o.GetUIStatus(context.Background(), db, "test-v1")

	if status.LatestIndexed != "66" {
		t.Fatalf("AI_FIX_REQUIRED [Stage 4]: Persistence finalization failed. UI LatestIndexed expected 66, got %s", status.LatestIndexed)
	}
}

// TestStage5_UI_Logic_Invariants 验证展示层：DTO 数据是否满足博彩级数学自洽
func TestStage5_UI_Logic_Invariants(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	o := GetOrchestrator()
	o.Reset()

	// 设置一个复杂状态
	o.UpdateChainHead(10000)
	o.Dispatch(CmdNotifyFetchProgress, uint64(9500))
	o.AdvanceDBCursor(9000)

	// 强制刷新一次快照
	o.Dispatch(CmdNotifyFetched, uint64(9500))

	time.Sleep(300 * time.Millisecond)
	status := o.GetUIStatus(context.Background(), db, "test-v1")

	// 1. 物理顺序约束
	snap := o.GetSnapshot()
	// Disk (9000) <= Memory (9500) <= Latest (10000)
	assert.LessOrEqual(t, snap.SyncedCursor, snap.FetchedHeight, "Disk progress cannot exceed memory progress")
	assert.LessOrEqual(t, snap.FetchedHeight, snap.LatestHeight, "Memory progress cannot exceed chain height")

	// 2. 数学自洽约束
	// SyncedHeight (9000) + SyncLag (1000) == LatestOnChain (10000)
	expectedLatest := 9000 + status.SyncLag
	assert.Equal(t, uint64(10000), uint64(expectedLatest), "Math paradox: Synced + Lag != Latest")
}
