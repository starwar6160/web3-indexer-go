package main

import (
	"context"
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
			currTx, currBlock := m.GetSnapshot()
			if lastTx > 0 {
				diffTx := currTx - lastTx
				diffBlock := currBlock - lastBlock
				currentTPS.Store(diffTx / 2) // 2秒间隔
				currentBPS.Store(diffBlock / 2)
			}
			lastTx = currTx
			lastBlock = currBlock
		}
	}
}
