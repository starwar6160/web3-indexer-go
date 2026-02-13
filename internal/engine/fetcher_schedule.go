package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
)

func (f *Fetcher) Schedule(ctx context.Context, start, end *big.Int) error {
	Logger.Info("📋 [Fetcher] Schedule 开始调度任务",
		slog.String("start_block", start.String()),
		slog.String("end_block", end.String()),
	)

	// QuickNode 优化：eth_getLogs 单次查询最多 2000 个块
	// 如果范围超过 2000，自动分批处理
	maxBlockRange := big.NewInt(2000)
	current := new(big.Int).Set(start)
	jobCount := 0

	for current.Cmp(end) <= 0 {
		batchEnd := new(big.Int).Add(current, maxBlockRange)
		if batchEnd.Cmp(end) > 0 {
			batchEnd = new(big.Int).Set(end)
		}

		// 调度当前批次的块
		for i := new(big.Int).Set(current); i.Cmp(batchEnd) <= 0; i.Add(i, big.NewInt(1)) {
			bn := new(big.Int).Set(i)

			select {
			case <-ctx.Done():
				Logger.Info("📋 [Fetcher] Schedule 被 context 取消",
					slog.Int("jobs_sent", jobCount),
				)
				return ctx.Err()
			case <-f.stopCh:
				Logger.Info("📋 [Fetcher] Schedule 被 stopCh 中断",
					slog.Int("jobs_sent", jobCount),
				)
				return fmt.Errorf("fetcher stopped")
			case f.jobs <- bn:
				jobCount++
				if jobCount%100 == 0 {
					Logger.Info("📋 [Fetcher] 已发送任务",
						slog.Int("count", jobCount),
						slog.String("current_block", bn.String()),
					)
				}
			}
		}

		// 移动到下一批
		current = new(big.Int).Add(batchEnd, big.NewInt(1))
	}

	Logger.Info("📋 [Fetcher] Schedule 完成，所有任务已发送",
		slog.Int("total_jobs", jobCount),
	)
	return nil
}