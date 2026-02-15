package engine

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/time/rate"
)

// MockEthClient for testing RPC pool
type MockEthClient struct {
	mock.Mock
}

func (m *MockEthClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Block), args.Error(1)
}

func (m *MockEthClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Header), args.Error(1)
}

func (m *MockEthClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, q)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.Log), args.Error(1)
}

func (m *MockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockEthClient) Close() {
	m.Called()
}

func TestRPCClientPool_NewRPCClientPool(t *testing.T) {
	// This test would require actual ethclient connections
	// For now, we'll test the structure with mock connections

	// Test with empty URLs
	_, err := NewRPCClientPool([]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no RPC URLs provided")
}

func TestRPCClientPool_GetHealthyNodeCount(t *testing.T) {
	// Create a mock pool for testing
	pool := &RPCClientPool{
		clients: []*rpcNode{
			{url: "node1", isHealthy: true},
			{url: "node2", isHealthy: false},
			{url: "node3", isHealthy: true},
		},
	}

	count := pool.GetHealthyNodeCount()
	assert.Equal(t, 2, count)
}

func TestRPCClientPool_Close(t *testing.T) {
	// Create a mock pool for testing
	pool := &RPCClientPool{
		clients: []*rpcNode{
			{url: "node1", isHealthy: true},
			{url: "node2", isHealthy: false},
		},
	}

	// Should not panic
	pool.Close()
}

func TestRPCNode_MarkUnhealthy(t *testing.T) {
	pool := &RPCClientPool{
		clients: []*rpcNode{
			{url: "node1", isHealthy: true, failCount: 0},
		},
	}

	node := pool.clients[0]
	assert.True(t, node.isHealthy)
	assert.Equal(t, 0, node.failCount)

	// Mark as unhealthy
	pool.markNodeUnhealthy(node)

	assert.False(t, node.isHealthy)
	assert.Equal(t, 1, node.failCount)
	assert.NotZero(t, node.lastError)
}

func TestRPCClientPool_Requests(t *testing.T) {
	mockEth := new(MockEthClient)
	pool := &RPCClientPool{
		clients: []*rpcNode{
			{url: "node1", client: mockEth, isHealthy: true},
		},
		size:        1,
		rateLimiter: rate.NewLimiter(rate.Inf, 0),
	}

	ctx := context.Background()

	t.Run("BlockByNumber", func(t *testing.T) {
		header := &types.Header{Number: big.NewInt(100)}
		block := types.NewBlockWithHeader(header)
		mockEth.On("BlockByNumber", mock.Anything, big.NewInt(100)).Return(block, nil).Once()

		result, err := pool.BlockByNumber(ctx, big.NewInt(100))
		assert.NoError(t, err)
		assert.Equal(t, big.NewInt(100), result.Number())
	})

	t.Run("HeaderByNumber", func(t *testing.T) {
		header := &types.Header{Number: big.NewInt(100)}
		mockEth.On("HeaderByNumber", mock.Anything, big.NewInt(100)).Return(header, nil).Once()

		result, err := pool.HeaderByNumber(ctx, big.NewInt(100))
		assert.NoError(t, err)
		assert.Equal(t, big.NewInt(100), result.Number)
	})

	t.Run("GetLatestBlockNumber", func(t *testing.T) {
		header := &types.Header{Number: big.NewInt(200)}
		mockEth.On("HeaderByNumber", mock.Anything, (*big.Int)(nil)).Return(header, nil).Once()

		result, err := pool.GetLatestBlockNumber(ctx)
		assert.NoError(t, err)
		assert.Equal(t, big.NewInt(200), result)
	})

	t.Run("FilterLogs", func(t *testing.T) {
		mockEth.On("FilterLogs", mock.Anything, mock.Anything).Return([]types.Log{{Index: 1}}, nil).Once()

		result, err := pool.FilterLogs(ctx, ethereum.FilterQuery{})
		assert.NoError(t, err)
		assert.Len(t, result, 1)
	})
}

func TestErrorDefinitions(t *testing.T) {
	assert.Equal(t, "reorg detected: parent hash mismatch", ErrReorgDetected.Error())
	assert.Equal(t, "reorg detected: need to refetch from common ancestor", ErrReorgNeedRefetch.Error())
}
