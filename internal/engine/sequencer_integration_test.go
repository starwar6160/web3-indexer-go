//go:build integration

package engine

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSequencerStartup 测试Sequencer启动和初始化
func TestSequencerStartup(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	// 🚀 使用本地 Anvil RPC 或环境变量中的 RPC
	rpcURL := os.Getenv("RPC_URLS")
	if rpcURL == "" {
		rpcURL = "http://localhost:8545" // 默认 fallback
	}

	rpcPool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 15*time.Second)
	if err != nil {
		t.Skipf("RPC not available: %v", err)
	}
	defer rpcPool.Close()

	processor := NewProcessor(db, rpcPool, 500, 1, false, "local")
	metrics := GetMetrics()

	startBlock := big.NewInt(100)
	chainID := int64(31337)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, metrics)
	require.NotNil(t, sequencer)
	assert.Equal(t, startBlock.String(), sequencer.GetExpectedBlock().String())
}

// TestSequencerBlockProcessing 测试Sequencer处理区块
func TestSequencerBlockProcessing(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	rpcPool, _ := NewRPCClientPool([]string{"http://localhost:8545"})
	processor := NewProcessor(db, rpcPool, 500, 1, false, "local")

	sequencer := NewSequencer(processor, big.NewInt(100), 1, make(chan BlockData), make(chan error, 1), GetMetrics())

	testBlock := createTestBlockForSequencer(big.NewInt(100))
	err = sequencer.handleBlock(context.Background(), BlockData{Block: testBlock})

	require.NoError(t, err)
	assert.Equal(t, "101", sequencer.GetExpectedBlock().String())
}

// TestSequencerWithRealRPC 测试Sequencer与真实RPC的集成
func TestSequencerWithRealRPC(t *testing.T) {
	rpcURL := os.Getenv("RPC_URLS")
	dbURL := os.Getenv("DATABASE_URL")
	if rpcURL == "" || dbURL == "" {
		t.Skip("RPC_URLS or DATABASE_URL not set")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	pool, err := NewRPCClientPoolWithTimeout([]string{rpcURL}, 15*time.Second)
	require.NoError(t, err)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = pool.GetLatestBlockNumber(ctx)
	if isNetworkEnvError(err) {
		t.Skipf("Skipping due to network error: %v", err)
	}
	require.NoError(t, err)

	processor := NewProcessor(db, pool, 500, 31337, false, "local")
	sequencer := NewSequencer(processor, big.NewInt(1), 31337, make(chan BlockData), make(chan error, 1), GetMetrics())
	require.NotNil(t, sequencer)
}

// 辅助函数：创建测试区块
func createTestBlockForSequencer(blockNumber *big.Int) *types.Block {
	header := &types.Header{
		Number:   blockNumber,
		GasLimit: 30000000,
		GasUsed:  15000000,
		// #nosec G115
		Time: uint64(time.Now().Unix()),
		Root: common.Hash{},
	}
	return types.NewBlockWithHeader(header).WithBody(types.Body{})
}
