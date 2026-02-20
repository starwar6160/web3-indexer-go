package engine

import (
	"log/slog"
	"sync"
	"time"
)

// LinearityGuard 时空连续性守卫
// 检测并处理"位点倒挂"问题：索引器领先于区块链
// 这是博彩级系统中最危险的状态之一

type LinearityGuard struct {
	mu sync.RWMutex

	// 状态
	lastCheckTime     time.Time
	consecutiveErrors int

	// 配置
	checkInterval     time.Duration
	maxAllowedLag     int64  // 允许的最大负滞后（索引器领先链的高度差）
	collapseThreshold uint64 // 触发强制坍缩的阈值

	// 组件引用
	orchestrator *Orchestrator
}

// NewLinearityGuard 创建时空连续性守卫
func NewLinearityGuard(orch *Orchestrator) *LinearityGuard {
	return &LinearityGuard{
		lastCheckTime:     time.Now(),
		checkInterval:     5 * time.Second,
		maxAllowedLag:     -1000, // 允许索引器领先最多1000个块
		collapseThreshold: 100,   // 超过100个块的倒挂就强制坍缩
		orchestrator:      orch,
	}
}

// CheckLinearity 检查时空连续性
// 核心逻辑：如果发现链的高度显著低于索引器游标，必须强制执行"状态坍缩"
func (g *LinearityGuard) CheckLinearity(rpcHeight uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 获取当前状态
	snap := g.orchestrator.GetSnapshot()

	// 计算"倒挂"程度（负值表示索引器领先）
	// #nosec G115 - diff only used for logging, overflow doesn't affect core logic
	diff := int64(rpcHeight) - int64(snap.SyncedCursor)

	// 🔥 情况1：链的高度回退了（Anvil 重置）
	if rpcHeight < snap.SyncedCursor {
		regression := snap.SyncedCursor - rpcHeight

		slog.Error("🚧 LINEARITY_CRASH: Chain regression detected!",
			"current_synced", snap.SyncedCursor,
			"actual_rpc", rpcHeight,
			"regression_blocks", regression,
			"diff", diff)

		// 判断是否需要强制坍缩
		if regression > g.collapseThreshold {
			g.triggerCollapse(rpcHeight, "chain_regression")
		} else {
			// 小幅回退，可能只是链的微小重组，增加错误计数
			g.consecutiveErrors++
			if g.consecutiveErrors >= 3 {
				g.triggerCollapse(rpcHeight, "consecutive_small_regression")
			}
		}
		return
	}

	// 🔥 情况2：链正常前进，重置错误计数
	if rpcHeight > snap.SyncedCursor {
		g.consecutiveErrors = 0
		return
	}

	// 🔥 情况3：链和索引器同步，检查停滞时间
	if rpcHeight == snap.SyncedCursor {
		if time.Since(g.lastCheckTime) > 30*time.Second {
			// 长时间停滞，可能是链已经停止出块
			slog.Warn("⚠️ LINEARITY_STALL: Chain and indexer both stalled",
				"height", rpcHeight,
				"stalled_for", time.Since(g.lastCheckTime))
		}
	}
}

// triggerCollapse 触发状态坍缩
// 这是最后的手段：强制重置所有位点到当前链尖
func (g *LinearityGuard) triggerCollapse(targetHeight uint64, reason string) {
	snap := g.orchestrator.GetSnapshot()

	slog.Error("💥 [Linearity] STATE COLLAPSE TRIGGERED!",
		"reason", reason,
		"from_synced", snap.SyncedCursor,
		"from_memory", snap.FetchedHeight,
		"to_height", targetHeight,
		"rollback_blocks", snap.SyncedCursor-targetHeight)

	// 🔥 直接操作状态而不是通过 Dispatch（避免测试环境阻塞）
	g.orchestrator.mu.Lock()
	g.orchestrator.state.SyncedCursor = targetHeight
	g.orchestrator.state.FetchedHeight = targetHeight
	g.orchestrator.state.LatestHeight = targetHeight
	g.orchestrator.state.TargetHeight = targetHeight
	g.orchestrator.state.SystemState = SystemStateHealing
	g.orchestrator.snapshot = g.orchestrator.state
	g.orchestrator.mu.Unlock()

	// 重置错误计数
	g.consecutiveErrors = 0
	g.lastCheckTime = time.Now()

	if targetHeight == 0 {
		slog.Info("✅ [Linearity] State collapsed to Block 0 - Fresh start")
	} else {
		slog.Info("✅ [Linearity] State collapsed to current chain head", "height", targetHeight)
	}
}

// IsTimeParadox 检查是否处于时空悖论状态
func (g *LinearityGuard) IsTimeParadox() bool {
	snap := g.orchestrator.GetSnapshot()
	// 简化判断：获取最新的链高度（通过 RPC）
	// 这里我们假设如果 SyncedCursor > 0 但 LatestHeight == 0，就是悖论
	return snap.SyncedCursor > 1000 && snap.LatestHeight == 0
}

// GetDiagnostic 获取诊断信息
func (g *LinearityGuard) GetDiagnostic() map[string]interface{} {
	snap := g.orchestrator.GetSnapshot()

	return map[string]interface{}{
		"synced_cursor":      snap.SyncedCursor,
		"latest_height":      snap.LatestHeight,
		"fetched_height":     snap.FetchedHeight,
		"consecutive_errors": g.consecutiveErrors,
		"last_check":         g.lastCheckTime.Format(time.RFC3339),
		"collapse_threshold": g.collapseThreshold,
		"is_time_paradox":    g.IsTimeParadox(),
	}
}

// Reset 手动重置守卫状态
func (g *LinearityGuard) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.consecutiveErrors = 0
	g.lastCheckTime = time.Now()

	slog.Info("🔄 [LinearityGuard] State reset")
}
