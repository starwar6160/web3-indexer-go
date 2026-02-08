package engine

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// RPCClientPool 多RPC节点池，支持轮询和故障转移
type RPCClientPool struct {
	clients []*rpcNode // 所有节点
	size    int32      // 节点数量
	index   int32      // 当前轮询索引
	mu      sync.RWMutex
}

// rpcNode 单个RPC节点封装
type rpcNode struct {
	url        string
	client     *ethclient.Client
	isHealthy  bool
	lastError  time.Time
	failCount  int
}

// NewRPCClientPool 创建RPC客户端池
func NewRPCClientPool(urls []string) (*RPCClientPool, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided")
	}

	pool := &RPCClientPool{
		clients: make([]*rpcNode, 0, len(urls)),
	}

	for _, url := range urls {
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Printf("Warning: failed to connect to %s: %v", url, err)
			continue
		}
		
		pool.clients = append(pool.clients, &rpcNode{
			url:       url,
			client:    client,
			isHealthy: true,
		})
	}

	if len(pool.clients) == 0 {
		return nil, fmt.Errorf("failed to connect to any RPC endpoint")
	}

	pool.size = int32(len(pool.clients))
	log.Printf("RPC Pool initialized with %d/%d nodes", len(pool.clients), len(urls))
	return pool, nil
}

// Close 关闭所有客户端连接
func (p *RPCClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	for _, node := range p.clients {
		if node.client != nil {
			node.client.Close()
		}
	}
	log.Printf("RPC Pool closed")
}

// getNextHealthyNode 获取下一个健康节点（轮询+健康检查）
func (p *RPCClientPool) getNextHealthyNode() *rpcNode {
	p.mu.RLock()
	defer p.mu.RUnlock()

	size := int(p.size)
	if size == 0 {
		return nil
	}

	// 轮询查找健康节点
	startIdx := int(atomic.AddInt32(&p.index, 1)) % size
	for i := 0; i < size; i++ {
		idx := (startIdx + i) % size
		node := p.clients[idx]
		
		if node.isHealthy {
			return node
		}
		
		// 如果节点不健康但已经超过5分钟，尝试恢复
		if time.Since(node.lastError) > 5*time.Minute {
			node.isHealthy = true
			node.failCount = 0
			log.Printf("RPC node %s recovered from error state", node.url)
			return node
		}
	}
	
	// 所有节点都不健康，返回nil让调用方处理
	return nil
}

// markNodeUnhealthy 标记节点为不健康状态
func (p *RPCClientPool) markNodeUnhealthy(node *rpcNode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	node.isHealthy = false
	node.lastError = time.Now()
	node.failCount++
	
	log.Printf("RPC node %s marked unhealthy (fail count: %d)", node.url, node.failCount)
}

// BlockByNumber 获取区块（带故障转移）
func (p *RPCClientPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}
		
		block, err := node.client.BlockByNumber(ctx, number)
		if err != nil {
			p.markNodeUnhealthy(node)
			continue
		}
		
		return block, nil
	}
	
	return nil, fmt.Errorf("all RPC nodes failed for BlockByNumber")
}

// HeaderByNumber 获取区块头（带故障转移）
func (p *RPCClientPool) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}
		
		header, err := node.client.HeaderByNumber(ctx, number)
		if err != nil {
			p.markNodeUnhealthy(node)
			continue
		}
		
		return header, nil
	}
	
	return nil, fmt.Errorf("all RPC nodes failed for HeaderByNumber")
}

// FilterLogs 过滤日志（带故障转移）
func (p *RPCClientPool) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}
		
		logs, err := node.client.FilterLogs(ctx, q)
		if err != nil {
			p.markNodeUnhealthy(node)
			continue
		}
		
		return logs, nil
	}
	
	return nil, fmt.Errorf("all RPC nodes failed for FilterLogs")
}

// GetHealthyNodeCount 返回健康节点数量
func (p *RPCClientPool) GetHealthyNodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	count := 0
	for _, node := range p.clients {
		if node.isHealthy {
			count++
		}
	}
	return count
}
