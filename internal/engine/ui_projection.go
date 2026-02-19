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
	MemorySync          string                 `json:"memory_sync"`   // 🚀 影子游标 (Fetcher 进度)
	SyncLag             int64                  `json:"sync_lag"`      // 物理滞后
	FetchLag            int64                  `json:"fetch_lag"`     // 扫描滞后
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

	// 1. 实时数据库统计 (带 5s 缓存以防高频请求压垮 DB)
	// 此处为简化逻辑直接查询，实际生产建议使用原子变量缓存
	var totalBlocks, totalTransfers int64
	_ = db.GetContext(ctx, &totalBlocks, "SELECT COUNT(*) FROM blocks")
	_ = db.GetContext(ctx, &totalTransfers, "SELECT COUNT(*) FROM transfers")

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

	// 4. 进度计算
	fetchProgress := 0.0
	if latest > 0 {
		fetchProgress = float64(snap.FetchedHeight) / float64(latest) * 100
	}
	syncProgress := 0.0
	if latest > 0 {
		syncProgress = float64(snap.SyncedCursor) / float64(latest) * 100
	}

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
	}
}