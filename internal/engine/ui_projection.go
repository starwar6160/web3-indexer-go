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

	// 🚀 视觉自愈
	latest := snap.LatestHeight
	if latest == 0 {
		if snap.FetchedHeight > 0 {
			latest = snap.FetchedHeight
		} else {
			latest = snap.SyncedCursor
		}
	}

	// 🚀 NEW: Get actual RPC height for reality check
	var rpcActual uint64
	var isInFuture bool
	var realityGap int64

	if o.fetcher != nil && o.fetcher.pool != nil {
		if tip, err := o.fetcher.pool.GetLatestBlockNumber(ctx); err == nil {
			rpcActual = tip.Uint64()
			realityGap = int64(rpcActual) - int64(snap.SyncedCursor)
			isInFuture = realityGap < 0
		}
	}

	// 1. 实时数据库统计 (带 5s 缓存以防高频请求压垮 DB)
	// 此处为简化逻辑直接查询，实际生产建议使用原子变量缓存
	var totalBlocks, totalTransfers int64
	if db != nil {
		if err := db.GetContext(ctx, &totalBlocks, "SELECT COUNT(*) FROM blocks"); err != nil {
			Logger.Debug("ui_projection_count_blocks_failed", "err", err)
		}
		if err := db.GetContext(ctx, &totalTransfers, "SELECT COUNT(*) FROM transfers"); err != nil {
			Logger.Debug("ui_projection_count_transfers_failed", "err", err)
		}
	}

	// 2. 逻辑自洽
	syncLag := SafeInt64Diff(latest, snap.SyncedCursor)
	if syncLag < 0 {
		syncLag = 0
	}

	// 3. 动态状态评估
	stateStr := snap.SystemState.String()
	if globalSnap.ResultsDepth > globalSnap.PipelineDepth*80/100 {
		stateStr = "pressure_limit"
	} else if syncLag > 1000 && GetMetrics().GetWindowBPS() < 1 {
		stateStr = "stalled"
	}

	// 🔥 检测时空悖论（索引器领先于链）
	warning := ""
	isTimeParadox := false

	if snap.SyncedCursor > latest && latest > 0 {
		isTimeParadox = true
		warning = "[!!] TIME_PARADOX: Indexer is ahead of Chain. Self-healing in progress..."
		stateStr = "time_paradox"
	} else if isInFuture {
		// 🚀 NEW: Enhanced paradox detection with RPC actual
		isTimeParadox = true
		warning = fmt.Sprintf("[!!] DETACHED: Indexer ahead of RPC reality by %d blocks. Realignment in progress...", -realityGap)
		stateStr = "detached"
	}

	// 4. 进度计算
	fetchProgress := 0.0
	if latest > 0 {
		fetchProgress = float64(snap.FetchedHeight) / float64(latest) * 100
	}
	syncProgress := 0.0
	if latest > 0 {
		syncProgress = float64(snap.SyncedCursor) / float64(latest) * 100
	}

	// 🚀 NEW: Parity status calculation
	parityStatus := "healthy"
	if isInFuture {
		parityStatus = "paradox_detected"
	} else if realityGap > 1000 {
		parityStatus = "lagging"
	}

	return UIStatusDTO{
		Version:             version,
		State:               stateStr,
		LatestBlock:         fmt.Sprintf("%d", latest),
		LatestIndexed:       fmt.Sprintf("%d", snap.SyncedCursor),
		TotalBlocks:         int64(snap.SyncedCursor), // #nosec G115 - SyncedCursor realistically fits in int64
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
		// 🔥 时空悖论警告
		Warning:       warning,
		IsTimeParadox: isTimeParadox,
		// 🚀 NEW: RPC reality fields
		RPCActual:    int64(rpcActual),
		RealityGap:   realityGap,
		ParityStatus: parityStatus,
	}
}
