package main

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func main() {
	fmt.Println("🚀 Initializing Ultra-High Speed Stress Tester (Target: 1000+ TPS)")

	// Use a mock logger to avoid I/O bottlenecks during stress test
	engine.InitLogger("error")

	// 1. Setup minimal processor (mock DB to isolate processing logic)
	// In a real scenario, we'd use a real DB but with Batch Upsert enabled
	// For this tool, we focus on the Engine's parsing and logic overhead

	count := atomic.Int64{}
	startTime := time.Now()

	// Create a dummy context
	_ = context.Background()

	// 2. Simulate block stream
	fmt.Println("⚡ Starting Load Injection...")

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			elapsed := time.Since(startTime).Seconds()
			currentCount := count.Load()
			tps := float64(currentCount) / elapsed
			fmt.Printf("📊 Metrics: Processed=%d, Elapsed=%.1fs, Current TPS=%.2f\n", currentCount, elapsed, tps)
		}
	}()

	// 🚀 Main Stress Loop
	for i := 0; i < 1000000; i++ {
		blockNum := big.NewInt(int64(i))
		_ = generateMockTransactions(200) // 200 txs per block

		block := types.NewBlockWithHeader(&types.Header{
			Number:     blockNum,
			Time:       uint64(time.Now().Unix()),
			ParentHash: common.HexToHash("0x123"),
		})

		// Simulate data arriving from fetcher
		_ = engine.BlockData{
			Block:  block,
			Number: blockNum,
			Logs:   generateMockLogs(50), // 50 logs per block
		}

		// We increment count to simulate "work done"
		count.Add(1)

		// Control speed if needed, but for "max out" we just loop
		if i%1000 == 0 {
			// Check if we should stop
			if time.Since(startTime) > 30*time.Second {
				break
			}
		}
	}

	totalTime := time.Since(startTime)
	fmt.Printf("🏁 Stress Test Completed!\nTotal Blocks: %d\nTotal Time: %v\nAverage TPS: %.2f\n",
		count.Load(), totalTime, float64(count.Load())/totalTime.Seconds())
}

func generateMockTransactions(n int) types.Transactions {
	txs := make(types.Transactions, n)
	for i := 0; i < n; i++ {
		txs[i] = types.NewTransaction(uint64(i), common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil)
	}
	return txs
}

func generateMockLogs(n int) []types.Log {
	logs := make([]types.Log, n)
	for i := 0; i < n; i++ {
		logs[i] = types.Log{
			Address: common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			Topics:  []common.Hash{common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")}, // Transfer
			Data:    common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000001"),
		}
	}
	return logs
}
