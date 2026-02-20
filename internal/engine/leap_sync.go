package engine

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// 🔥 Leap-Sync: 强制对齐巨大高度差距
// 当检测到 rpc_actual 与 mem_latest 差距超过阈值时，
// 直接强制对齐，而不是试图逐个块追赶

const (
	// LeapSyncThreshold: 触发跃迁同步的阈值（1000 个块）
	LeapSyncThreshold = 1000

	// LeapSyncMaxGap: 最大可接受差距（10万个块）
	// 超过此值说明系统已严重滞后，需要立即干预
	LeapSyncMaxGap = 100000
)

// LeapSyncMonitor 跃迁同步监控器
type LeapSyncMonitor struct {
	enabled       bool
	lastCheck     time.Time
	checkInterval time.Duration
}

// NewLeapSyncMonitor 创建跃迁同步监控器
func NewLeapSyncMonitor() *LeapSyncMonitor {
	return &LeapSyncMonitor{
		enabled:       true,
		checkInterval: 30 * time.Second, // 每30秒检查一次
	}
}

// Start 启动监控循环
func (lsm *LeapSyncMonitor) Start(orch *Orchestrator, rpcPool RPCClient) {
	if !lsm.enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(lsm.checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			lsm.checkAndLeap(orch, rpcPool)
		}
	}()

	slog.Info("🚀 Leap-Sync monitor started", "threshold", LeapSyncThreshold, "interval", lsm.checkInterval)
}

// checkAndLeap 检查并执行跃迁同步
func (lsm *LeapSyncMonitor) checkAndLeap(orch *Orchestrator, rpcPool RPCClient) {
	if orch == nil || rpcPool == nil {
		return
	}

	// 获取当前状态
	snap := orch.GetSnapshot()

	// 获取 RPC 实际高度
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rpcHeightBig, err := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		slog.Debug("🚀 Leap-Sync: RPC query failed", "err", err)
		return
	}
	rpcHeight := rpcHeightBig.Uint64()

	// 计算差距
	var gap uint64
	if rpcHeight > snap.LatestHeight {
		gap = rpcHeight - snap.LatestHeight
	} else {
		// 如果 mem_latest > rpc_actual，这是"未来人"状态，需要坍缩
		if snap.LatestHeight > rpcHeight+LeapSyncThreshold {
			slog.Warn("⚡ LEAP-SYNC: Future Human detected! Collapsing to reality",
				"mem_latest", snap.LatestHeight,
				"rpc_actual", rpcHeight,
				"gap", snap.LatestHeight-rpcHeight)
			orch.SnapToReality(rpcHeight)
			return
		}
		return
	}

	// 检查是否需要跃迁同步
	if gap >= LeapSyncThreshold {
		slog.Warn("⚡ LEAP-SYNC: Large gap detected, triggering leap",
			"mem_latest", snap.LatestHeight,
			"rpc_actual", rpcHeight,
			"gap", gap,
			"threshold", LeapSyncThreshold)

		// 执行跃迁同步
		lsm.performLeap(orch, rpcHeight, gap)
	}
}

// performLeap 执行跃迁同步
func (lsm *LeapSyncMonitor) performLeap(orch *Orchestrator, targetHeight uint64, gap uint64) {
	// 获取当前内存状态
	snap := orch.GetSnapshot()

	// 🚀 跃迁策略：
	// 1. 如果差距极大 (> 10000)，直接重置到目标高度
	// 2. 如果差距中等 (1000-10000)，尝试快速追赶

	if gap > 10000 {
		// 极端情况：直接强制对齐
		slog.Error("⚡ LEAP-SYNC: Extreme gap! Forcing reality alignment",
			"from", snap.LatestHeight,
			"to", targetHeight,
			"gap", gap)

		orch.SnapToReality(targetHeight)

		// 记录事件
		orch.DispatchLog("WARN", "Leap-Sync: Extreme gap forced reality alignment",
			"from", snap.LatestHeight,
			"to", targetHeight,
			"gap", gap)

	} else {
		// 中等差距：更新 LatestHeight 但不重置其他状态
		// 这允许 Fetcher 继续从当前位置追赶
		slog.Info("⚡ LEAP-SYNC: Moderate gap, updating target",
			"current_latest", snap.LatestHeight,
			"new_target", targetHeight,
			"gap", gap)

		// 通过 UpdateChainHead 更新高度
		orch.UpdateChainHead(targetHeight)
	}
}

// GetMemoryStats 获取内存统计（用于决策）
func GetMemoryStats() (totalMB, availMB uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 获取系统内存信息（近似值）
	totalMB = m.Sys / (1024 * 1024)
	availMB = (m.Sys - m.HeapAlloc) / (1024 * 1024)

	return totalMB, availMB
}
