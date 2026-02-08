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

// rpcNode 单个RPC节点封装
type rpcNode struct {
	url       string
	client    *ethclient.Client
	isHealthy bool
	lastError time.Time
	failCount int
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
		rateLimiter: rate.NewLimiter(5, 10), // 令牌桶限速器：每秒5个请求，突发容量10个
	}

	for _, url := range urls {
		// 使用标准方法创建 ethclient
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
		log.Printf("Warning: no RPC nodes connected initially, will retry later")
		// 返回空池，但允许后续重试
		pool.size = 0
		return pool, nil
	}

	pool.size = int32(len(pool.clients))
	log.Printf("RPC Pool initialized with %d/%d nodes (timeout: %v)", len(pool.clients), len(urls), timeout)
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

	// 记录详细的节点错误日志
	LogRPCRequestFailed("node_unhealthy", node.url, fmt.Errorf("fail_count: %d", node.failCount))

	log.Printf("RPC node %s marked unhealthy (fail count: %d)", node.url, node.failCount)
}

// BlockByNumber 获取区块（带故障转移和限速）
func (p *RPCClientPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	for attempts := 0; attempts < int(p.size); attempts++ {
		// 令牌桶限速：等待令牌
		if err := p.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter error: %w", err)
		}

		node := p.getNextHealthyNode()
		if node == nil {
			// 所有节点都不健康，记录告警
			log.Printf("⚠️ CRITICAL: All RPC nodes are unhealthy!")
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// 为单次 RPC 请求设置超时（10秒）
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		block, err := node.client.BlockByNumber(reqCtx, number)
		cancel()

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

		// 为单次 RPC 请求设置超时（10秒）
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		header, err := node.client.HeaderByNumber(reqCtx, number)
		cancel()

		if err != nil {
			p.markNodeUnhealthy(node)
			continue
		}

		return header, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for HeaderByNumber")
}

// FilterLogs 过滤日志（带故障转移和限速）
func (p *RPCClientPool) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	for attempts := 0; attempts < int(p.size); attempts++ {
		// 令牌桶限速：等待令牌
		if err := p.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter error: %w", err)
		}

		node := p.getNextHealthyNode()
		if node == nil {
			// 所有节点都不健康，记录告警
			log.Printf("⚠️ CRITICAL: All RPC nodes are unhealthy!")
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// 为单次 RPC 请求设置超时（10秒）
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		logs, err := node.client.FilterLogs(reqCtx, q)
		cancel()

		if err != nil {
			p.markNodeUnhealthy(node)
			continue
		}

		return logs, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for FilterLogs")
}

// GetLatestBlockNumber 获取链上最新块高（用于增量同步）
func (p *RPCClientPool) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			log.Printf("⚠️ CRITICAL: All RPC nodes are unhealthy!")
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// 为单次 RPC 请求设置超时（10秒）
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		header, err := node.client.HeaderByNumber(reqCtx, nil) // nil = latest
		cancel()

		if err != nil {
			p.markNodeUnhealthy(node)
			continue
		}

		return header.Number, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for GetLatestBlockNumber")
}

// CheckHealth 检查所有节点的健康状态
func (p *RPCClientPool) CheckHealth() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	healthyNodes := 0
	for _, node := range p.clients {
		if node.isHealthy {
			// 执行简单的健康检查
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := node.client.BlockNumber(ctx)
			cancel()

			if err != nil {
				node.isHealthy = false
				node.failCount++
				node.lastError = time.Now()
				log.Printf("RPC node %s marked unhealthy (fail count: %d)", node.url, node.failCount)
			} else {
				healthyNodes++
			}
		} else {
			// 不健康的节点可以尝试恢复
			if time.Since(node.lastError) > 30*time.Second {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := node.client.BlockNumber(ctx)
				cancel()

				if err == nil {
					node.isHealthy = true
					log.Printf("RPC node %s recovered", node.url)
					healthyNodes++
				}
			}
		}
	}

	return healthyNodes > 0
}

// WaitForHealthy 等待直到有健康节点或超时
func (p *RPCClientPool) WaitForHealthy(timeout time.Duration) bool {
	start := time.Now()
	for {
		if p.CheckHealth() {
			return true
		}

		if time.Since(start) > timeout {
			return false
		}

		time.Sleep(5 * time.Second)
		log.Printf("Waiting for healthy RPC nodes...")
	}
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

// GetTotalNodeCount 返回总节点数量
func (p *RPCClientPool) GetTotalNodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}
