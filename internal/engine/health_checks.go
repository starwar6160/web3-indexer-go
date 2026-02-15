package engine

import (
	"context"
	"fmt"
	"time"
)

// checkDatabase 检查数据库连接
func (h *HealthServer) checkDatabase(ctx context.Context) Check {
	start := time.Now()

	err := h.db.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		return Check{
			Status:  "unhealthy",
			Message: err.Error(),
			Latency: latency.String(),
		}
	}

	return Check{
		Status:  "healthy",
		Latency: latency.String(),
	}
}

// checkRPC 检查 RPC 连接
func (h *HealthServer) checkRPC(ctx context.Context) Check {
	start := time.Now()

	healthyCount := h.rpcPool.GetHealthyNodeCount()
	totalCount := h.rpcPool.GetTotalNodeCount()

	// 获取最新区块号
	header, err := h.rpcPool.HeaderByNumber(ctx, nil)
	latency := time.Since(start)

	if err != nil {
		return Check{
			Status:  "unhealthy",
			Message: fmt.Sprintf("rpc_nodes: %d/%d healthy, error: %s", healthyCount, totalCount, err.Error()),
			Latency: latency.String(),
		}
	}

	const healthyStatus = "healthy"
	status := healthyStatus
	if healthyCount < totalCount {
		status = "degraded"
	}

	return Check{
		Status:  status,
		Message: fmt.Sprintf("rpc_nodes: %d/%d healthy, latest_block: %s", healthyCount, totalCount, header.Number.String()),
		Latency: latency.String(),
	}
}

// checkSequencer 检查 Sequencer 状态
func (h *HealthServer) checkSequencer(_ context.Context) Check {
	if h.sequencer == nil {
		return Check{
			Status:  "unhealthy",
			Message: "sequencer not initialized",
		}
	}

	bufferSize := h.sequencer.GetBufferSize()
	expectedBlock := h.sequencer.GetExpectedBlock()

	// 如果 buffer 过大，可能有问题
	if bufferSize > 500 {
		return Check{
			Status:  "degraded",
			Message: fmt.Sprintf("buffer_size: %d (high)", bufferSize),
		}
	}

	return Check{
		Status:  "healthy",
		Message: fmt.Sprintf("expected_block: %s, buffer_size: %d", expectedBlock.String(), bufferSize),
	}
}

// checkFetcher 检查 Fetcher 状态
func (h *HealthServer) checkFetcher(_ context.Context) Check {
	if h.fetcher == nil {
		return Check{
			Status:  "unhealthy",
			Message: "fetcher not initialized",
		}
	}

	// 检查是否暂停
	if h.fetcher.IsPaused() {
		return Check{
			Status:  "degraded",
			Message: "fetcher paused (likely reorg handling)",
		}
	}

	return Check{
		Status:  "healthy",
		Message: "fetcher running",
	}
}
