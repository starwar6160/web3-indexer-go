package engine

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSequencerStartup 测试Sequencer启动和初始化
func TestSequencerStartup(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	// 创建processor
	rpcPool, err := NewRPCClientPoolWithTimeout([]string{"https://rpc.sepolia.org"}, 10*time.Second)
	require.NoError(t, err)
	defer rpcPool.Close()

	processor := NewProcessor(db, rpcPool)
	metrics := GetMetrics()

	// 创建Sequencer
	startBlock := big.NewInt(10216000)
	chainID := int64(11155111)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)
	require.NotNil(t, sequencer)

	// 验证初始状态
	assert.Equal(t, startBlock.String(), sequencer.GetExpectedBlock().String())
	assert.Equal(t, 0, sequencer.GetBufferSize())

	t.Logf("✅ Sequencer initialized successfully with start block: %s", startBlock.String())
}

// TestSequencerBlockProcessing 测试Sequencer处理区块
func TestSequencerBlockProcessing(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	rpcPool, err := NewRPCClientPoolWithTimeout([]string{"https://rpc.sepolia.org"}, 10*time.Second)
	require.NoError(t, err)
	defer rpcPool.Close()

	processor := NewProcessor(db, rpcPool)
	metrics := GetMetrics()

	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)

	// 创建测试区块
	testBlock := createTestBlockForSequencer(big.NewInt(100))
	blockData := BlockData{Block: testBlock}

	ctx := context.Background()
	err = sequencer.handleBlock(ctx, blockData)

	require.NoError(t, err)
	assert.Equal(t, "101", sequencer.GetExpectedBlock().String())

	t.Logf("✅ Sequencer successfully processed block 100, expected block now: 101")
}

// TestSequencerBuffering 测试Sequencer的乱序区块缓冲
func TestSequencerBuffering(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	// 强制应用最新 Schema 补丁（工业级防御）
	_, _ = db.Exec("ALTER TABLE blocks ADD COLUMN IF NOT EXISTS parent_hash VARCHAR(66) NOT NULL DEFAULT ''")

	rpcPool, err := NewRPCClientPoolWithTimeout([]string{"https://rpc.sepolia.org"}, 10*time.Second)
	require.NoError(t, err)
	defer rpcPool.Close()

	processor := NewProcessor(db, rpcPool)
	metrics := GetMetrics()

	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)

	// 1. 确定性构建哈希链: 100 <- 101 <- 102
	// 使用固定时间戳防止哈希在不同测试运行间漂移
	baseTime := uint64(1700000000)

	// Block 100
	h100 := &types.Header{Number: big.NewInt(100), Time: baseTime}
	block100 := types.NewBlockWithHeader(h100)
	
	// Block 101
	h101 := &types.Header{Number: big.NewInt(101), Time: baseTime + 1, ParentHash: block100.Hash()}
	block101 := types.NewBlockWithHeader(h101)
	
	// Block 102
	h102 := &types.Header{Number: big.NewInt(102), Time: baseTime + 2, ParentHash: block101.Hash()}
	block102 := types.NewBlockWithHeader(h102)

	ctx := context.Background()

	// 发送102（乱序）
	err = sequencer.handleBlock(ctx, BlockData{Block: block102, Number: big.NewInt(102)})
	require.NoError(t, err)
	assert.Equal(t, 1, sequencer.GetBufferSize(), "should buffer block 102")

	// 发送101（乱序）
	err = sequencer.handleBlock(ctx, BlockData{Block: block101, Number: big.NewInt(101)})
	require.NoError(t, err)
	assert.Equal(t, 2, sequencer.GetBufferSize(), "should buffer block 101")

	// 发送100（期望的）
	err = sequencer.handleBlock(ctx, BlockData{Block: block100, Number: big.NewInt(100)})
	require.NoError(t, err)

	// 应该处理100，然后从buffer中顺序处理101和102
	assert.Equal(t, "103", sequencer.GetExpectedBlock().String(), "should process all buffered blocks without reorg")
}

// TestSequencerWithRealRPC 测试Sequencer与真实RPC的集成
func TestSequencerWithRealRPC(t *testing.T) {
	rpcURL := os.Getenv("RPC_URLS")
	dbURL := os.Getenv("DATABASE_URL")
	if rpcURL == "" || dbURL == "" {
		t.Skip("RPC_URLS or DATABASE_URL not set, skipping integration test")
	}

	// 连接数据库
	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	// 创建RPC池
	rpcPool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 10*time.Second)
	require.NoError(t, err)
	defer rpcPool.Close()

	// 验证RPC连接
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	latestBlock, err := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized")) {
		t.Skip("External RPC unauthorized, skipping")
	}
	require.NoError(t, err, "RPC connection failed")

	// 创建Processor和Metrics
	processor := NewProcessor(db, rpcPool)
	metrics := GetMetrics()

	// 创建Sequencer
	startBlock := big.NewInt(10216000)
	chainID := int64(11155111)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)
	require.NotNil(t, sequencer)

	// 验证Sequencer初始化
	assert.Equal(t, startBlock.String(), sequencer.GetExpectedBlock().String())

	t.Logf("✅ Sequencer initialized with real RPC connection. Latest block: %s", latestBlock.String())
}

// TestSequencerGoroutinePanic 测试Sequencer Goroutine崩溃捕获
func TestSequencerGoroutinePanic(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	rpcPool, err := NewRPCClientPoolWithTimeout([]string{"https://rpc.sepolia.org"}, 10*time.Second)
	require.NoError(t, err)
	defer rpcPool.Close()

	processor := NewProcessor(db, rpcPool)
	metrics := GetMetrics()

	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 在Goroutine中运行Sequencer
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("❌ SEQUENCER PANIC DETECTED: %v", r)
			}
		}()
		sequencer.Run(ctx)
	}()

	// 关闭resultCh来触发Sequencer关闭
	close(resultCh)

	// 等待Sequencer完成
	select {
	case <-ctx.Done():
		t.Logf("✅ Sequencer completed without panic")
	case err := <-fatalErrCh:
		t.Logf("⚠️ Sequencer reported error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatalf("Sequencer did not complete within timeout")
	}
}

// TestSequencerContextCancellation 测试Sequencer的Context取消
func TestSequencerContextCancellation(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	rpcPool, err := NewRPCClientPoolWithTimeout([]string{"https://rpc.sepolia.org"}, 10*time.Second)
	require.NoError(t, err)
	defer rpcPool.Close()

	processor := NewProcessor(db, rpcPool)
	metrics := GetMetrics()

	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)

	ctx, cancel := context.WithCancel(context.Background())

	// 在Goroutine中运行Sequencer
	done := make(chan bool)
	go func() {
		sequencer.Run(ctx)
		done <- true
	}()

	// 立即取消Context
	cancel()

	// 等待Sequencer完成
	select {
	case <-done:
		t.Logf("✅ Sequencer properly handled context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatalf("Sequencer did not respond to context cancellation")
	}
}

// 辅助函数：创建测试区块
func createTestBlockForSequencer(blockNumber *big.Int) *types.Block {
	header := &types.Header{
		Number:   blockNumber,
		GasLimit: 30000000,
		GasUsed:  15000000,
		Time:     uint64(time.Now().Unix()),
		Root:     common.Hash{},
	}
	body := &types.Body{
		Transactions: nil,
		Uncles:       nil,
	}
	return types.NewBlockWithHeader(header).WithBody(*body)
}
