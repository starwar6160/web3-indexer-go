package engine

import (
	"fmt"
	"time"
)

// UIStatusDTO 是专门给网页看的数据契约 (Shadow Snapshot)
type UIStatusDTO struct {
	Version          string                 `json:"version"`
	State            string                 `json:"state"` // LIVE, PRESSURE_LIMIT, STALLED, DEGRADED
	LatestChain      string                 `json:"latest_chain"`
	MemorySync       string                 `json:"memory_sync"`   // 🚀 影子游标 (Fetcher 进度)
	DiskSync         string                 `json:"disk_sync"`     // 物理游标 (DB 进度)
	SyncLag          int64                  `json:"sync_lag"`      // 物理滞后
	FetchLag         int64                  `json:"fetch_lag"`     // 扫描滞后 (Latest - Memory)
	Progress         float64                `json:"progress"`      // 物理进度
	FetchProgress    float64                `json:"fetch_progress"` // 扫描进度
	BPS              float64                `json:"bps"`
	TPS              float64                `json:"tps"`
	Health           bool                   `json:"health"`
	JobsDepth        int                    `json:"jobs_depth"`
	JobsCapacity     int                    `json:"jobs_capacity"`
	ResultsDepth     int                    `json:"results_depth"`
	ResultsCapacity  int                    `json:"results_capacity"`
	SafetyBuffer     uint64                 `json:"safety_buffer"`
	LastLog          map[string]interface{} `json:"last_log"`
	UpdatedAt        string                 `json:"updated_at"`
	Fingerprint      string                 `json:"fingerprint"`
}

// GetUIStatus 将复杂的内部状态投影为简洁的 UI 对象
// 彻底解决 API Handler 圈复杂度过高的问题 (Ref: gocyclo)
func (o *Orchestrator) GetUIStatus(version string) UIStatusDTO {
	snap := o.GetSnapshot()
	globalSnap := GetGlobalState().Snapshot()
	maxJobs, maxResults, _ := GetGlobalState().GetCapacity()

	// 1. 逻辑自洽：安全计算滞后 (修复 G115 溢出风险)
	syncLag := SafeInt64Diff(snap.LatestHeight, snap.SyncedCursor)
	if syncLag < 0 {
		syncLag = 0
	}

	fetchLag := SafeInt64Diff(snap.LatestHeight, snap.FetchedHeight)
	if fetchLag < 0 {
		fetchLag = 0
	}

	// 2. 动态状态评估 (博彩级实时感)
	stateStr := snap.SystemState.String()
	// 如果 Results 队列积压超过 80%
	if globalSnap.ResultsDepth > globalSnap.PipelineDepth*80/100 {
		stateStr = "pressure_limit"
	} else if syncLag > 1000 && GetMetrics().GetWindowBPS() < 1 {
		stateStr = "stalled"
	}

	// 3. 扫描进度计算
	fetchProgress := 0.0
	if snap.LatestHeight > 0 {
		fetchProgress = float64(snap.FetchedHeight) / float64(snap.LatestHeight) * 100
		if fetchProgress > 100.0 {
			fetchProgress = 100.0
		}
	}

	return UIStatusDTO{
		Version:         version,
		State:           stateStr,
		LatestChain:     fmt.Sprintf("%d", snap.LatestHeight),
		MemorySync:      fmt.Sprintf("%d", snap.FetchedHeight),
		DiskSync:        fmt.Sprintf("%d", snap.SyncedCursor),
		SyncLag:         syncLag,
		FetchLag:        fetchLag,
		Progress:        snap.Progress,
		FetchProgress:   fetchProgress,
		BPS:             GetMetrics().GetWindowBPS(),
		TPS:             GetMetrics().GetWindowTPS(),
		Health:          stateStr != "stalled",
		JobsDepth:       int(globalSnap.JobsQueueDepth),
		JobsCapacity:    int(maxJobs),
		ResultsDepth:    int(globalSnap.ResultsDepth),
		ResultsCapacity: int(maxResults),
		SafetyBuffer:    snap.SafetyBuffer,
		LastLog:         snap.LogEntry,
		UpdatedAt:       snap.UpdatedAt.Format(time.RFC3339),
		Fingerprint:     "Yokohama-Lab-Primary",
	}
}