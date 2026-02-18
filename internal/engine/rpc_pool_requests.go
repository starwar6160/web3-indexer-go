package engine

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

// --- Common internal methods for both pools ---

func (p *EnhancedRPCClientPool) getNextHealthyNode() *rpcNode {
	p.mu.RLock()
	defer p.mu.RUnlock()

	size := int(p.size)
	if size == 0 {
		return nil
	}

	totalWeight := 0
	var healthyNodes []*rpcNode
	for _, node := range p.clients {
		if node.isHealthy || time.Now().After(node.retryAfter) {
			healthyNodes = append(healthyNodes, node)
			w := node.weight
			if w <= 0 {
				w = 1
			}
			totalWeight += w
		}
	}

	if len(healthyNodes) == 0 {
		return nil
	}

	count := atomic.AddInt64(&p.requestCount, 1)
	val := int(count % int64(totalWeight))

	current := 0
	for _, node := range healthyNodes {
		w := node.weight
		if w <= 0 {
			w = 1
		}
		current += w
		if val < current {
			return node
		}
	}

	return healthyNodes[0]
}

func (p *RPCClientPool) getNextHealthyNode() *rpcNode {
	p.mu.RLock()
	defer p.mu.RUnlock()

	size := int(p.size)
	if size == 0 {
		return nil
	}

	// Basic round robin for legacy pool
	idx := atomic.AddInt32(&p.index, 1)
	return p.clients[int(idx)%size]
}

// --- EnhancedRPCClientPool methods ---

func (p *EnhancedRPCClientPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	if p.isTestnetMode {
		if err := p.enforceSyncBatchLimit(); err != nil {
			return nil, err
		}
		if p.globalRateLimiter != nil {
			if err := p.globalRateLimiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("global rate limiter error: %w", err)
			}
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		if p.isTestnetMode {
			limiter := p.nodeRateLimiters[node.url]
			if limiter != nil {
				if err := limiter.Wait(ctx); err != nil {
					return nil, fmt.Errorf("node rate limiter error: %w", err)
				}
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		block, err := node.client.BlockByNumber(reqCtx, number)
		cancel()

		p.incrementRequestCount(node.url, "BlockByNumber")

		if err != nil {
			p.handleRPCError(node, err)
			continue
		}

		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
		}

		return block, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for BlockByNumber")
}

func (p *EnhancedRPCClientPool) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if p.isTestnetMode {
		if err := p.enforceSyncBatchLimit(); err != nil {
			return nil, err
		}
		if p.globalRateLimiter != nil {
			if err := p.globalRateLimiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("global rate limiter error: %w", err)
			}
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		if p.isTestnetMode {
			limiter := p.nodeRateLimiters[node.url]
			if limiter != nil {
				if err := limiter.Wait(ctx); err != nil {
					return nil, fmt.Errorf("node rate limiter error: %w", err)
				}
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		header, err := node.client.HeaderByNumber(reqCtx, number)
		cancel()

		p.incrementRequestCount(node.url, "HeaderByNumber")

		if err != nil {
			p.handleRPCError(node, err)
			continue
		}

		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
		}

		return header, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for HeaderByNumber")
}

func (p *EnhancedRPCClientPool) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if p.isTestnetMode {
		if err := p.enforceSyncBatchLimit(); err != nil {
			return nil, err
		}
		if p.globalRateLimiter != nil {
			if err := p.globalRateLimiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("global rate limiter error: %w", err)
			}
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		if p.isTestnetMode {
			limiter := p.nodeRateLimiters[node.url]
			if limiter != nil {
				if err := limiter.Wait(ctx); err != nil {
					return nil, fmt.Errorf("node rate limiter error: %w", err)
				}
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		logs, err := node.client.FilterLogs(reqCtx, q)
		cancel()

		p.incrementRequestCount(node.url, "FilterLogs")

		if err != nil {
			p.handleRPCError(node, err)
			continue
		}

		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
		}

		return logs, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for FilterLogs")
}

func (p *EnhancedRPCClientPool) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	if p.isTestnetMode {
		if p.globalRateLimiter != nil {
			if err := p.globalRateLimiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("global rate limiter error: %w", err)
			}
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		if p.isTestnetMode {
			limiter := p.nodeRateLimiters[node.url]
			if limiter != nil {
				if err := limiter.Wait(ctx); err != nil {
					return nil, fmt.Errorf("node rate limiter error: %w", err)
				}
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		header, err := node.client.HeaderByNumber(reqCtx, nil)
		cancel()

		p.incrementRequestCount(node.url, "GetLatestBlockNumber")

		if err != nil {
			p.handleRPCError(node, err)
			continue
		}

		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
		}

		return header.Number, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for GetLatestBlockNumber")
}

func (p *EnhancedRPCClientPool) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if p.isTestnetMode {
		if p.globalRateLimiter != nil {
			if err := p.globalRateLimiter.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		res, err := node.client.CallContract(reqCtx, msg, blockNumber)
		cancel()

		p.incrementRequestCount(node.url, "CallContract")
		if err != nil {
			p.handleRPCError(node, err)
			continue
		}
		return res, nil
	}
	return nil, fmt.Errorf("all RPC nodes failed for CallContract")
}

func (p *EnhancedRPCClientPool) GetClientForMetadata() LowLevelRPCClient {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.clients) == 0 {
		return nil
	}

	for _, node := range p.clients {
		if node.isHealthy {
			return node.client
		}
	}
	return p.clients[0].client
}

// --- RPCClientPool (Legacy) methods ---

func (p *RPCClientPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	node := p.getNextHealthyNode()
	if node == nil {
		return nil, fmt.Errorf("no RPC nodes available")
	}
	return node.client.BlockByNumber(ctx, number)
}

func (p *RPCClientPool) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	node := p.getNextHealthyNode()
	if node == nil {
		return nil, fmt.Errorf("no RPC nodes available")
	}
	return node.client.HeaderByNumber(ctx, number)
}

func (p *RPCClientPool) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	node := p.getNextHealthyNode()
	if node == nil {
		return nil, fmt.Errorf("no RPC nodes available")
	}
	return node.client.FilterLogs(ctx, q)
}

func (p *RPCClientPool) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	header, err := p.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	return header.Number, nil
}

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

func (p *RPCClientPool) GetTotalNodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

func (p *RPCClientPool) SetRateLimit(_ float64, _ int) {}
func (p *RPCClientPool) Close() {
	for _, node := range p.clients {
		node.client.Close()
	}
}
