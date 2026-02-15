package engine

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

// RPCClient 定义RPC客户端接口，用于测试和生产代码
type RPCClient interface {
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	GetLatestBlockNumber(ctx context.Context) (*big.Int, error)
	GetHealthyNodeCount() int
	GetTotalNodeCount() int
	Close()
}

// 确保RPCClientPool实现了RPCClient接口
var _ RPCClient = (*RPCClientPool)(nil)

// 确保EnhancedRPCClientPool也实现了RPCClient接口
var _ RPCClient = (*EnhancedRPCClientPool)(nil)
