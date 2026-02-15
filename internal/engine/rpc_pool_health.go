package engine

import (
	"context"
	"log"
	"time"

	"golang.org/x/time/rate"
)

// StartHealthCheck starts a background goroutine to periodically check the health of RPC nodes.
func (p *RPCClientPool) StartHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.CheckHealth()
			}
		}
	}()
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

	// Report to Prometheus
	GetMetrics().UpdateRPCHealthyNodes("default", healthyNodes)

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

// SetRateLimit 动态设置限速 (令牌桶)
func (p *RPCClientPool) SetRateLimit(rps int, burst int) {
	p.rateLimiter.SetLimit(rate.Limit(rps))
	p.rateLimiter.SetBurst(burst)
	log.Printf("RPC Pool rate limit updated: %d RPS, %d Burst", rps, burst)
}
