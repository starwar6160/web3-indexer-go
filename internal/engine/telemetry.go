package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LogPulse 输出一行纯 JSON 的系统快照，专供 AI 诊断工具解析
// 包含了博彩级的自洽性核心指标
func (o *Orchestrator) LogPulse(ctx context.Context) {
	status := o.GetUIStatus(ctx, nil, "v2.2.0") // 遥测不需要实时 DB count

	strategyName := "unknown"
	if o.strategy != nil {
		strategyName = o.strategy.Name()
	}

	pulse := map[string]interface{}{
		"ts":         time.Now().UnixMilli(),
		"tag":        "AI_DIAGNOSTIC",
		"state":      status.State,
		"latest":     status.LatestBlock,
		"mem_sync":   status.MemorySync,
		"disk_sync":  status.LatestIndexed,
		"lag":        status.SyncLag,
		"jobs":       status.JobsDepth,
		"bps":        status.BPS,
		"strategy":   strategyName,
		"buffer_pct": float64(status.ResultsDepth) / float64(status.ResultsCapacity) * 100,
	}

	data, err := json.Marshal(pulse)
	if err == nil {
		// 使用 fmt.Println 确保输出到 stdout，方便脚本捕获
		fmt.Printf("📊 %s\n", string(data))
	}
}