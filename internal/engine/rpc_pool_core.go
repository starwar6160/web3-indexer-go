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
	"golang.org/x/time/rate"
)

// RPCClientPool 多RPC节点池，支持轮询和故障转移
type RPCClientPool struct {
	clients     []*rpcNode // 所有节点
	size        int32      // 节点数量
	index       int32      // 当前轮询索引
	mu          sync.RWMutex
	rateLimiter *rate.Limiter // 令牌桶限速器
}

// LowLevelRPCClient defines the subset of ethclient.Client methods used by rpcNode
type LowLevelRPCClient interface {
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	BlockNumber(ctx context.Context) (uint64, error)
	Close()
}

// rpcNode 单个RPC节点封装
type rpcNode struct {
	url        string
	client     LowLevelRPCClient
	isHealthy  bool
	lastError  time.Time
	failCount  int
	retryAfter time.Time // 下次允许尝试的时间
	weight     int       // 节点权重 (e.g., Alchemy=3, Infura=1)
}

// NewRPCClientPool 创建RPC客户端池
func NewRPCClientPool(urls []string) (*RPCClientPool, error) {
	return NewRPCClientPoolWithTimeout(urls, 10*time.Second)
}

// NewRPCClientPoolWithTimeout 创建带自定义超时的RPC节点池
func NewRPCClientPoolWithTimeout(urls []string, timeout time.Duration) (*RPCClientPool, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided")
	}

	pool := &RPCClientPool{
		clients:     make([]*rpcNode, 0, len(urls)),
		rateLimiter: rate.NewLimiter(rate.Limit(20), 40), // 默认 20 RPS, 40 Burst
	}

	for _, url := range urls {
		// 使用标准方法创建 ethclient
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Printf("Warning: failed to connect to %s: %v", url, err)
			continue
		}

		// 创建节点对象，初始状态为不健康，需要通过健康检查
		node := &rpcNode{
			url:       url,
			client:    client,
			isHealthy: false, // 初始状态为不健康
		}

		// 立即进行健康检查
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err = client.HeaderByNumber(ctx, nil) // 获取最新块头来验证连接
		cancel()

		if err != nil {
			log.Printf("Health check failed for %s: %v", url, err)
			node.isHealthy = false
			node.lastError = time.Now()
		} else {
			log.Printf("Health check passed for %s", url)
			node.isHealthy = true
		}

		pool.clients = append(pool.clients, node)
	}

	if len(pool.clients) == 0 {
		log.Printf("Warning: no RPC nodes connected initially, will retry later")
		// 返回空池，但允许后续重试
		pool.size = 0
		return pool, nil
	}

	pool.size = int32(len(pool.clients))
	healthyCount := 0
	for _, node := range pool.clients {
		if node.isHealthy {
			healthyCount++
		}
	}
	log.Printf("RPC Pool initialized with %d/%d nodes healthy (timeout: %v)", healthyCount, len(pool.clients), timeout)
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

// getNextHealthyNode 获取下一个健康节点（轮询+指数退避）
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

		// 检查指数退避是否结束
		if time.Now().After(node.retryAfter) {
			// 尝试将其视为可用的
			return node
		}
	}

	return nil
}

// markNodeUnhealthy 标记节点为不健康状态，并应用指数退避
func (p *RPCClientPool) markNodeUnhealthy(node *rpcNode) {
	p.mu.Lock()
	defer p.mu.Unlock()

	node.isHealthy = false
	node.lastError = time.Now()
	node.failCount++

	// 指数退避：1s, 2s, 4s, 8s, 16s, 32s, max 60s
	backoffSec := 1 << (node.failCount - 1)
	if backoffSec > 60 {
		backoffSec = 60
	}
	node.retryAfter = node.lastError.Add(time.Duration(backoffSec) * time.Second)

	// 记录详细的节点错误日志
	LogRPCRequestFailed("node_unhealthy", node.url, fmt.Errorf("fail_count: %d, retry_after: %v", node.failCount, node.retryAfter.Format("15:04:05")))

	log.Printf("RPC node %s marked unhealthy (fail count: %d, retry after %ds)", node.url, node.failCount, backoffSec)
}
