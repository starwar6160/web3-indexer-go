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

	// 工业级建议：Range 查询步长
	// 正常同步使用 50 个块，Catch-up 时可以更大
	batchSize := big.NewInt(50)
	current := new(big.Int).Set(start)
	jobCount := 0

	for current.Cmp(end) <= 0 {
		batchEnd := new(big.Int).Add(current, batchSize)
		if batchEnd.Cmp(end) > 0 {
			batchEnd = new(big.Int).Set(end)
		}

		job := FetchJob{
			Start: new(big.Int).Set(current),
			End:   new(big.Int).Set(batchEnd),
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.stopCh:
			return fmt.Errorf("fetcher stopped")
		case f.jobs <- job:
			jobCount++
		}

		// 移动到下一批
		current = new(big.Int).Add(batchEnd, big.NewInt(1))
	}

	Logger.Info("📋 [Fetcher] Schedule 完成，所有任务已发送",
		slog.Int("total_jobs", jobCount),
	)
	return nil
}
