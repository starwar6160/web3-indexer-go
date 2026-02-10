package engine

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEthClient for testing RPC pool
type MockEthClient struct {
	mock.Mock
}

func (m *MockEthClient) BlockByNumber(ctx context.Context, number interface{}) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Block), args.Error(1)
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

func TestErrorDefinitions(t *testing.T) {
	assert.Equal(t, "reorg detected: parent hash mismatch", ErrReorgDetected.Error())
	assert.Equal(t, "reorg detected: need to refetch from common ancestor", ErrReorgNeedRefetch.Error())
}
