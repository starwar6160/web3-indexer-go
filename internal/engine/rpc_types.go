package engine

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// rpcNode represents a single RPC node in the pool
type rpcNode struct {
	url        string
	client     *ethclient.Client
	isHealthy  bool
	failCount  int
	lastError  time.Time
	retryAfter time.Time
	weight     int
}

// RPCClientPool represents a pool of RPC nodes (Legacy/Basic version)
type RPCClientPool struct {
	clients []*rpcNode
	size    int32
	index   int32
	mu      sync.RWMutex
}

// LowLevelRPCClient defines the minimal interface needed for metadata fetch
type LowLevelRPCClient interface {
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
}
