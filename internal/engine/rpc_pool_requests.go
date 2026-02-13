package engine

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

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

		// 成功请求，恢复健康状态
		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
			log.Printf("RPC node %s recovered to healthy", node.url)
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

		// 成功请求，恢复健康状态
		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
			log.Printf("RPC node %s recovered to healthy", node.url)
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

		// 成功请求，恢复健康状态
		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
			log.Printf("RPC node %s recovered to healthy", node.url)
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

		// 成功请求，恢复健康状态
		if !node.isHealthy {
			p.mu.Lock()
			node.isHealthy = true
			node.failCount = 0
			p.mu.Unlock()
			log.Printf("RPC node %s recovered to healthy", node.url)
		}

		return header.Number, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for GetLatestBlockNumber")
}