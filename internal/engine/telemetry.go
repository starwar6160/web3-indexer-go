package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// LogPulse 输出一行纯 JSON 的系统快照，专供 AI 诊断工具解析
func (o *Orchestrator) LogPulse(ctx context.Context) {
	status := o.GetUIStatus(ctx, nil, "v2.2.0")

	// 🚀 获取物理真实的 RPC 高度进行对比
	var rpcActual uint64
	if o.fetcher != nil && o.fetcher.pool != nil {
		if tip, err := o.fetcher.pool.GetLatestBlockNumber(ctx); err == nil {
			rpcActual = tip.Uint64()
		}
	}

	// 🚀 计算位点差距
	latestNum, _ := new(big.Int).SetString(status.LatestBlock, 10)
	memNum, _ := new(big.Int).SetString(status.MemorySync, 10)

	// 🚀 NEW: Enhanced reality gap calculation
	realityGap := int64(0)
	isInFuture := false
	parityCheck := "healthy"

	if latestNum != nil && memNum != nil {
		// #nosec G115 - realityGap only used for display, overflow doesn't affect core logic
		realityGap = int64(rpcActual) - memNum.Int64()
		isInFuture = realityGap < 0

		// Parity health check
		// #nosec G115 - comparison only used for logging/display, overflow doesn't affect core logic
		if memNum.Int64() > int64(rpcActual) {
			parityCheck = "paradox_detected"
			// #nosec G115 - calculation only used for logging/display, overflow doesn't affect core logic
		} else if int64(rpcActual)-memNum.Int64() > 1000 {
			parityCheck = "lagging"
		}
	}

	diff := int64(0)
	if latestNum != nil && memNum != nil {
		diff = latestNum.Int64() - memNum.Int64()
	}

	pulse := map[string]interface{}{
		"ts":         time.Now().UnixMilli(),
		"tag":        "AI_DIAGNOSTIC",
		"rpc_actual": rpcActual,
		"mem_latest": status.LatestBlock,
		"is_desync":  fmt.Sprintf("%d", rpcActual) != status.LatestBlock,
		"state":      status.State,
		"mem_sync":   status.MemorySync,
		"disk_sync":  status.LatestIndexed,
		"diff":       diff,
		"lag":        status.SyncLag,
		"bps":        status.BPS,
		"strategy":   strategyName(o.strategy),
		// 🚀 NEW: Reality collapse fields
		"reality_gap":  realityGap,  // Can be negative (future)
		"is_in_future": isInFuture,  // Boolean flag
		"parity_check": parityCheck, // Health status
	}

	data, err := json.Marshal(pulse)
	if err == nil {
		// 使用 fmt.Println 确保输出到 stdout，方便脚本捕获
		fmt.Printf("📊 %s\n", string(data))
	}
}

func strategyName(s Strategy) string {
	if s == nil {
		return "unknown"
	}
	return s.Name()
}
