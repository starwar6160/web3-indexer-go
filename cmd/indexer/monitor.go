package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
	"web3-indexer-go/internal/engine"
)

// 全局性能快照 (用于计算 TPS)
var (
	currentTPS atomic.Uint64
	currentBPS atomic.Uint64 // Blocks Per Second
)

func startPerformanceMonitor(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastTx, lastBlock uint64
	m := engine.GetMetrics()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currTx, currBlock, _, _ := m.GetSnapshot()
			if lastTx > 0 {
				diffTx := currTx - lastTx
				diffBlock := currBlock - lastBlock
				// 2秒间隔，计算每秒速率
				currentTPS.Store(diffTx / 2)
				currentBPS.Store(diffBlock / 2)

				// 调试日志（每 30 秒打印一次）
				if diffBlock > 0 || diffTx > 0 {
					slog.Info("performance_update",
						"blocks_in_2s", diffBlock,
						"bps", diffBlock/2,
						"transfers_in_2s", diffTx,
						"tps", diffTx/2,
					)
				}
			}
			lastTx = currTx
			lastBlock = currBlock
		}
	}
}
