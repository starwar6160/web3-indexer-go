package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// UIStatusDTO 是专门给网页看的数据契约 (Shadow Snapshot)
type UIStatusDTO struct {
	Version             string                 `json:"version"`
	State               string                 `json:"state"` // LIVE, PRESSURE_LIMIT, STALLED, DEGRADED
	LatestBlock         string                 `json:"latest_block"`
	LatestIndexed       string                 `json:"latest_indexed"`
	TotalBlocks         int64                  `json:"total_blocks"`
	TotalTransfers      int64                  `json:"total_transfers"`
	MemorySync          string                 `json:"memory_sync"` // 🚀 影子游标 (Fetcher 进度)
	SyncLag             int64                  `json:"sync_lag"`    // 物理滞后
	FetchLag            int64                  `json:"fetch_lag"`   // 扫描滞后
	SyncProgressPercent float64                `json:"sync_progress_percent"`
	FetchProgress       float64                `json:"fetch_progress"`
	TPS                 float64                `json:"tps"`
	BPS                 float64                `json:"bps"`
	Health              bool                   `json:"health"`
	JobsDepth           int                    `json:"jobs_depth"`
	JobsCapacity        int                    `json:"jobs_capacity"`
	ResultsDepth        int                    `json:"results_depth"`
	ResultsCapacity     int                    `json:"results_capacity"`
	SafetyBuffer        uint64                 `json:"safety_buffer"`
	LastLog             map[string]interface{} `json:"last_log"`
	UpdatedAt           string                 `json:"updated_at"`
	LastPulse           int64                  `json:"last_pulse"`
	Fingerprint         string                 `json:"fingerprint"`
	// 🔥 时空悖论警告
	Warning       string `json:"warning,omitempty"`         // 警告信息
	IsTimeParadox bool   `json:"is_time_paradox,omitempty"` // 是否处于时空悖论

	// 🚀 NEW: RPC reality fields
	RPCActual    int64  `json:"rpc_actual,omitempty"`    // RPC 实际高度
	RealityGap   int64  `json:"reality_gap,omitempty"`   // 现实差距（可为负）
	ParityStatus string `json:"parity_status,omitempty"` // 奇偶校验状态
}

// GetUIStatus 将复杂的内部状态投影为简洁的 UI 对象
func (o *Orchestrator) GetUIStatus(ctx context.Context, db *sqlx.DB, version string) UIStatusDTO {
	snap := o.GetSnapshot()
	globalSnap := GetGlobalState().Snapshot()
	maxJobs, maxResults, _ := GetGlobalState().GetCapacity()

	// 🚀 视觉自愈：获取基准最新高度
	latest := o.getVisualLatestHeight(snap)

	// 🚀 Reality Check: 获取 RPC 真实高度
	rpcActual, isInFuture, realityGap := o.checkRPCReality(ctx, snap)

	// 1. 实时数据库统计
	totalBlocks, totalTransfers := o.getDBStats(ctx, db, snap)

	// 2. 逻辑自洽与状态评估
	syncLag := SafeInt64Diff(latest, snap.SyncedCursor)
	if syncLag < 0 {
		syncLag = 0
	}
	stateStr := o.evaluateStatus(snap, globalSnap, syncLag, isInFuture)

	// 3. 悖论警告与奇偶状态
	warning, isTimeParadox := o.detectParadox(snap, latest, isInFuture, realityGap)
	parityStatus := o.calculateParity(isInFuture, realityGap)

	// 4. 进度计算
	fetchProgress := o.calculateProgress(snap.FetchedHeight, latest)
	syncProgress := o.calculateProgress(snap.SyncedCursor, latest)

	// Safe cast for display fields
	rpcActualInt, _ := SafeCastUint64ToInt64(rpcActual) // nolint:errcheck // display only, ignore overflow

	return UIStatusDTO{
		Version:             version,
		State:               stateStr,
		LatestBlock:         fmt.Sprintf("%d", latest),
		LatestIndexed:       fmt.Sprintf("%d", snap.SyncedCursor),
		TotalBlocks:         totalBlocks,
		TotalTransfers:      totalTransfers,
		MemorySync:          fmt.Sprintf("%d", snap.FetchedHeight),
		SyncLag:             syncLag,
		FetchLag:            SafeInt64Diff(latest, snap.FetchedHeight),
		SyncProgressPercent: syncProgress,
		FetchProgress:       fetchProgress,
		TPS:                 GetMetrics().GetWindowTPS(),
		BPS:                 GetMetrics().GetWindowBPS(),
		Health:              stateStr != "stalled",
		JobsDepth:           int(globalSnap.JobsQueueDepth),
		JobsCapacity:        int(maxJobs),
		ResultsDepth:        int(globalSnap.ResultsDepth),
		ResultsCapacity:     int(maxResults),
		SafetyBuffer:        snap.SafetyBuffer,
		LastLog:             snap.LogEntry,
		UpdatedAt:           snap.UpdatedAt.Format(time.RFC3339),
		LastPulse:           time.Now().UnixMilli(),
		Fingerprint:         "Yokohama-Lab-Primary",
		Warning:             warning,
		IsTimeParadox:       isTimeParadox,
		RPCActual:           rpcActualInt,
		RealityGap:          realityGap,
		ParityStatus:        parityStatus,
	}
}

func (o *Orchestrator) getVisualLatestHeight(snap CoordinatorState) uint64 {
	if snap.LatestHeight > 0 {
		return snap.LatestHeight
	}
	if snap.FetchedHeight > 0 {
		return snap.FetchedHeight
	}
	return snap.SyncedCursor
}

func (o *Orchestrator) checkRPCReality(ctx context.Context, snap CoordinatorState) (uint64, bool, int64) {
	if o.fetcher == nil || o.fetcher.pool == nil {
		return 0, false, 0
	}
	tip, err := o.fetcher.pool.GetLatestBlockNumber(ctx)
	if err != nil {
		return 0, false, 0
	}
	rpcActual := tip.Uint64()

	// Safe gap calculation
	cursorInt, _ := SafeCastUint64ToInt64(snap.SyncedCursor) // nolint:errcheck // logic verified
	actualInt, _ := SafeCastUint64ToInt64(rpcActual)         // nolint:errcheck // logic verified
	realityGap := actualInt - cursorInt

	return rpcActual, realityGap < 0, realityGap
}

func (o *Orchestrator) getDBStats(ctx context.Context, db *sqlx.DB, snap CoordinatorState) (int64, int64) {
	var totalBlocks, totalTransfers int64
	if db != nil {
		if err := db.GetContext(ctx, &totalBlocks, "SELECT COUNT(*) FROM blocks"); err != nil {
			Logger.Debug("ui_projection_count_blocks_failed", "err", err)
		}
		if err := db.GetContext(ctx, &totalTransfers, "SELECT COUNT(*) FROM transfers"); err != nil {
			Logger.Debug("ui_projection_count_transfers_failed", "err", err)
		}
	}
	// 尝试安全转换同步游标作为总块数参考
	if castVal, err := SafeCastUint64ToInt64(snap.SyncedCursor); err == nil && castVal > totalBlocks {
		totalBlocks = castVal
	}
	return totalBlocks, totalTransfers
}

func (o *Orchestrator) evaluateStatus(snap CoordinatorState, globalSnap Snapshot, syncLag int64, isInFuture bool) string {
	stateStr := snap.SystemState.String()
	if globalSnap.ResultsDepth > globalSnap.PipelineDepth*80/100 {
		return "pressure_limit"
	}
	if syncLag > 1000 && GetMetrics().GetWindowBPS() < 1 {
		return "stalled"
	}
	if isInFuture {
		return "detached"
	}
	return stateStr
}

func (o *Orchestrator) detectParadox(snap CoordinatorState, latest uint64, isInFuture bool, realityGap int64) (string, bool) {
	if snap.SyncedCursor > latest && latest > 0 {
		return "[!!] TIME_PARADOX: Indexer is ahead of Chain. Self-healing in progress...", true
	}
	if isInFuture {
		return fmt.Sprintf("[!!] DETACHED: Indexer ahead of RPC reality by %d blocks. Realignment in progress...", -realityGap), true
	}
	return "", false
}

func (o *Orchestrator) calculateParity(isInFuture bool, realityGap int64) string {
	if isInFuture {
		return "paradox_detected"
	}
	if realityGap > 1000 {
		return "lagging"
	}
	return "healthy"
}

func (o *Orchestrator) calculateProgress(current uint64, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(current) / float64(total) * 100
}
