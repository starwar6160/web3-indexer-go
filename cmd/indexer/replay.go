package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"web3-indexer-go/internal/engine"
)

// RunReplayMode 启动高保真回放模式
func RunReplayMode(ctx context.Context, path string, speed float64, processor *engine.Processor) error {
	slog.Info("🎬 [REPLAY] Initializing replay machine", "file", path, "speed", speed)

	// 1. 构造回放源
	source, err := engine.NewLz4ReplaySource(path, speed)
	if err != nil {
		return fmt.Errorf("failed to open replay file: %w", err)
	}
	defer source.Close()

	// 2. 获取进度报告器
	metrics := engine.GetMetrics()

	// 3. 回放主循环
	slog.Info("🚀 [REPLAY] Playback started. Firehose activated.")

	// 每次回放 10 块
	batchSize := big.NewInt(10)
	currentBlock := big.NewInt(0) // 从头开始扫描文件

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("🛑 [REPLAY] Playback interrupted by user")
			return nil
		case <-ticker.C:
			// 从文件中提取下一批区块
			end := new(big.Int).Add(currentBlock, batchSize)
			blocks, err := source.FetchLogs(ctx, currentBlock, end)

			if err != nil {
				slog.Error("❌ [REPLAY] Read error", "err", err)
				return err
			}

			if len(blocks) == 0 {
				// 检查是否到达 EOF (目前 FetchLogs 没返回 EOF 错误，而是空列表)
				// 我们暂时通过进度判断是否结束
				if source.GetProgress() >= 99.9 {
					slog.Info("🏁 [REPLAY] End of trajectory reached. Mission accomplished.")
					return nil
				}
				// 没数据但没结束，继续往后探
				currentBlock.Add(end, big.NewInt(1))
				continue
			}

			// 4. 将数据灌入处理器
			if err := processor.ProcessBatch(ctx, blocks, 0); err != nil {
				slog.Error("❌ [REPLAY] Processing failed", "err", err)
			}

			// 5. 更新进度指标
			progress := source.GetProgress()
			metrics.UpdateReplayProgress(progress)

			// 6. 打印控制台进度条
			renderProgressBar(progress, blocks[len(blocks)-1].Number.Uint64())

			// 更新当前游标
			lastBn := blocks[len(blocks)-1].Number
			currentBlock.Add(lastBn, big.NewInt(1))
		}
	}
}

func renderProgressBar(progress float64, currentBlock uint64) {
	barLen := 40
	filled := int(float64(barLen) * progress / 100)
	if filled > barLen {
		filled = barLen
	}

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := filled; i < barLen; i++ {
		bar += "░"
	}

	fmt.Printf("🎬 [REPLAY] [%s] %.2f%% | Block: %d", bar, progress, currentBlock)
	if progress >= 100 {
		fmt.Println()
	}
}
