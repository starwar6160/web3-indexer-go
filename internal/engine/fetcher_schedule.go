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

	// 🔥 横滨实验室：上游背压检测
	// 检查下游水位线，避免无界调度导致背压
	jobsDepth := len(f.jobs)
	resultsDepth := len(f.Results)
	maxJobsCapacity := cap(f.jobs)
	maxResultsCapacity := cap(f.Results)

	// 水位线阈值
	jobsWatermark := maxJobsCapacity * 80 / 100       // 80%
	resultsWatermark := maxResultsCapacity * 80 / 100 // 80%

	if jobsDepth > jobsWatermark {
		Logger.Warn("🚫 [Fetcher] SCHEDULE_BLOCKED: Jobs queue too deep",
			slog.Int("jobs_depth", jobsDepth),
			slog.Int("jobs_watermark", jobsWatermark),
			slog.Int("jobs_capacity", maxJobsCapacity),
			slog.Int("results_depth", resultsDepth))
		return fmt.Errorf("fetcher jobs queue backpressure: depth=%d/%d", jobsDepth, maxJobsCapacity)
	}

	if resultsDepth > resultsWatermark {
		Logger.Warn("🚫 [Fetcher] SCHEDULE_BLOCKED: Results channel too deep",
			slog.Int("results_depth", resultsDepth),
			slog.Int("results_watermark", resultsWatermark),
			slog.Int("results_capacity", maxResultsCapacity),
			slog.Int("jobs_depth", jobsDepth))
		return fmt.Errorf("fetcher results channel backpressure: depth=%d/%d", resultsDepth, maxResultsCapacity)
	}

	// 🔥 检查 Sequencer buffer 深度
	// 横滨实验室：提升阈值至 2000 (128G RAM 可承受)
	if f.sequencer != nil {
		seqBufferSize := f.sequencer.GetBufferSize()
		if seqBufferSize > 2000 {
			Logger.Warn("🚫 [Fetcher] SCHEDULE_BLOCKED: Sequencer buffer too deep",
				slog.Int("sequencer_buffer_size", seqBufferSize),
				slog.Int("jobs_depth", jobsDepth),
				slog.Int("results_depth", resultsDepth))
			return fmt.Errorf("sequencer buffer backpressure: size=%d", seqBufferSize)
		}
	}

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
