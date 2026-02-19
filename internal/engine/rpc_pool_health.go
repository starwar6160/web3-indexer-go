package engine

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

// CalculateOptimalRPS 根据环境自动计算最优 RPS
func CalculateOptimalRPS(rpcURL string, currentLag int64, userConfigRPS int) float64 {
	var policyRPS float64
	isLocal := strings.Contains(rpcURL, "localhost") || strings.Contains(rpcURL, "127.0.0.1")

	if isLocal {
		policyRPS = 500.0
	} else if strings.Contains(rpcURL, "infura.io") || strings.Contains(rpcURL, "quiknode.pro") {
		policyRPS = 15.0
	} else if strings.Contains(rpcURL, "public.com") {
		policyRPS = 5.0
	} else {
		policyRPS = 10.0
	}

	if currentLag > 1000 && !isLocal {
		policyRPS *= 2.0
	}

	if userConfigRPS > 0 {
		configured := float64(userConfigRPS)
		if isLocal {
			return configured
		}
		return math.Min(policyRPS, configured)
	}
	return policyRPS
}

// handleRPCError handles errors from RPC nodes, specifically looking for 429s
func (p *EnhancedRPCClientPool) handleRPCError(node *rpcNode, err error) {
	if err == nil {
		return
	}

	errStr := err.Error()
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "too many requests") || strings.Contains(errStr, "limit exceeded") {
		log.Printf("🛑 [CIRCUIT BREAKER] %s returned 429, entering 5-minute cooldown", node.url)
		p.mu.Lock()
		node.isHealthy = false
		node.lastError = time.Now()
		node.retryAfter = time.Now().Add(5 * time.Minute)
		p.mu.Unlock()

		if p.metrics != nil {
			p.metrics.UpdateRPCHealthyNodes("enhanced", p.GetHealthyNodeCount())
		}
	} else {
		p.markNodeUnhealthy(node)
	}
}

// markNodeUnhealthy marks a node as unhealthy with exponential backoff
func (p *EnhancedRPCClientPool) markNodeUnhealthy(node *rpcNode) {
	p.mu.Lock()
	defer p.mu.Unlock()

	node.isHealthy = false
	node.lastError = time.Now()
	node.failCount++

	backoffSec := int(math.Min(math.Pow(2, float64(node.failCount-1)), 60))
	node.retryAfter = node.lastError.Add(time.Duration(backoffSec) * time.Second)

	LogRPCRequestFailed("node_unhealthy", node.url, fmt.Errorf("fail_count: %d, retry_after: %v", node.failCount, node.retryAfter.Format("15:04:05")))
	log.Printf("RPC node %s marked unhealthy (fail count: %d, retry after %ds)", node.url, node.failCount, backoffSec)
}

// incrementRequestCount increments the global request counter
func (p *EnhancedRPCClientPool) incrementRequestCount(nodeURL, method string) {
	atomic.AddInt64(&p.requestCount, 1)

	if p.quotaMonitor != nil {
		p.quotaMonitor.Inc()
	}

	if p.metrics != nil {
		duration := time.Since(p.lastResetTime)
		p.metrics.RecordRPCRequest(nodeURL, method, duration, true)
	}
}

// enforceSyncBatchLimit enforces the maximum sync batch size.
// The sleep is performed after releasing the lock so that concurrent
// RPC callers are not blocked while we wait.
func (p *EnhancedRPCClientPool) enforceSyncBatchLimit() {
	if !p.isTestnetMode {
		return
	}

	p.batchMutex.Lock()
	p.currentSyncBatch++
	shouldSleep := p.currentSyncBatch > 50
	if shouldSleep {
		p.currentSyncBatch = 0
	}
	p.batchMutex.Unlock()

	if shouldSleep {
		time.Sleep(200 * time.Millisecond)
	}
}

// GetRequestCount returns the total number of RPC requests made
func (p *EnhancedRPCClientPool) GetRequestCount() int64 {
	return atomic.LoadInt64(&p.requestCount)
}

// ResetRequestCount resets the request counter
func (p *EnhancedRPCClientPool) ResetRequestCount() {
	atomic.StoreInt64(&p.requestCount, 0)
	p.lastResetTime = time.Now()
}

// StartHealthCheck starts a background goroutine for periodic health checks
func (p *EnhancedRPCClientPool) StartHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		defer ticker.Stop()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("EnhancedHealthCheck goroutine panic: %v", r)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.checkHealth()
			}
		}
	}()
}

// checkHealth checks all nodes' health status
func (p *EnhancedRPCClientPool) checkHealth() {
	p.mu.Lock()
	defer p.mu.Unlock()

	healthyNodes := 0
	for _, node := range p.clients {
		if node.isHealthy {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := node.client.BlockNumber(ctx)
			cancel()

			if err != nil {
				node.isHealthy = false
				node.failCount++
				node.lastError = time.Now()
				log.Printf("Enhanced RPC node %s marked unhealthy (fail count: %d)", node.url, node.failCount)
			} else {
				healthyNodes++
			}
		} else if time.Since(node.lastError) > 30*time.Second {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := node.client.BlockNumber(ctx)
			cancel()

			if err == nil {
				node.isHealthy = true
				log.Printf("Enhanced RPC node %s recovered", node.url)
				healthyNodes++
			}
		}
	}
	GetMetrics().UpdateRPCHealthyNodes("enhanced", healthyNodes)
}
