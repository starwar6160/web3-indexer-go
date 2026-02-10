package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRPCPoolConnection 测试RPC池与真实Sepolia节点的连接
func TestRPCPoolConnection(t *testing.T) {
	// 从环境变量读取RPC URL
	rpcURL := os.Getenv("RPC_URLS")
	if rpcURL == "" {
		t.Skip("RPC_URLS not set, skipping integration test")
	}

	// 创建RPC池
	pool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 10*time.Second)
	require.NoError(t, err, "failed to create RPC pool")
	require.NotNil(t, pool, "RPC pool should not be nil")
	defer pool.Close()

	// 测试获取最新区块号
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	latestBlock, err := pool.GetLatestBlockNumber(ctx)
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "429")) {
		t.Skip("External RPC unauthorized or rate limited")
	}
	require.NoError(t, err, "failed to get latest block number")
	require.NotNil(t, latestBlock, "latest block should not be nil")

	// 验证区块号合理性
	assert.Greater(t, latestBlock.Int64(), int64(10000000), "block number should be greater than 10000000")
	t.Logf("✅ Successfully connected to RPC node. Latest block: %s", latestBlock.String())
}

// TestRPCPoolHeaderByNumber 测试获取特定区块头
func TestRPCPoolHeaderByNumber(t *testing.T) {
	rpcURL := os.Getenv("RPC_URLS")
	if rpcURL == "" {
		t.Skip("RPC_URLS not set, skipping integration test")
	}

	pool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 10*time.Second)
	require.NoError(t, err)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 先获取最新区块
	latestBlock, err := pool.GetLatestBlockNumber(ctx)
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "429")) {
		t.Skip("External RPC unauthorized or rate limited")
	}
	require.NoError(t, err)

	// 获取该区块的头信息
	header, err := pool.HeaderByNumber(ctx, latestBlock)
	require.NoError(t, err, "failed to get header by number")
	require.NotNil(t, header, "header should not be nil")

	assert.Equal(t, latestBlock, header.Number, "block number should match")
	t.Logf("✅ Successfully retrieved header for block %s", header.Number.String())
}

// TestRPCPoolMultipleRequests 测试多个连续请求
func TestRPCPoolMultipleRequests(t *testing.T) {
	rpcURL := os.Getenv("RPC_URLS")
	if rpcURL == "" {
		t.Skip("RPC_URLS not set, skipping integration test")
	}

	pool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 10*time.Second)
	require.NoError(t, err)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 发起多个请求
	for i := 0; i < 5; i++ {
		latestBlock, err := pool.GetLatestBlockNumber(ctx)
		require.NoError(t, err, "request %d failed", i+1)
		assert.NotNil(t, latestBlock)
		t.Logf("Request %d: Block %s", i+1, latestBlock.String())
	}

	t.Logf("✅ Successfully completed 5 consecutive RPC requests")
}

// TestRPCPoolRateLimiting 测试限流器是否工作
func TestRPCPoolRateLimiting(t *testing.T) {
	rpcURL := os.Getenv("RPC_URLS")
	if rpcURL == "" {
		t.Skip("RPC_URLS not set, skipping integration test")
	}

	pool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 10*time.Second)
	require.NoError(t, err)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 测试限流：发起10个请求，应该需要至少2秒（5 rps限制）
	startTime := time.Now()
	for i := 0; i < 10; i++ {
		_, err := pool.GetLatestBlockNumber(ctx)
		require.NoError(t, err)
	}
	elapsed := time.Since(startTime)

	// 限流器设置为5 rps，10个请求应该至少需要2秒
	assert.GreaterOrEqual(t, elapsed, 1500*time.Millisecond, "rate limiting should enforce minimum duration")
	t.Logf("✅ Rate limiting verified: 10 requests took %v (expected >= 2s)", elapsed)
}

// TestRPCPoolHealthCheck 测试节点健康检查
func TestRPCPoolHealthCheck(t *testing.T) {
	rpcURL := os.Getenv("RPC_URLS")
	if rpcURL == "" {
		t.Skip("RPC_URLS not set, skipping integration test")
	}

	pool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 10*time.Second)
	require.NoError(t, err)
	defer pool.Close()

	// 检查健康节点数
	healthyCount := pool.GetHealthyNodeCount()
	assert.Greater(t, healthyCount, 0, "should have at least one healthy node")
	t.Logf("✅ Health check passed: %d healthy nodes", healthyCount)
}
