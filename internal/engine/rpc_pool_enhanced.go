package engine

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"web3-indexer-go/internal/monitor"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/time/rate"
)

// EnhancedRPCClientPool extends the basic RPC pool with advanced rate limiting and monitoring
type EnhancedRPCClientPool struct {
	clients           []*rpcNode
	size              int32
	index             int32
	mu                sync.RWMutex
	globalRateLimiter *rate.Limiter
	nodeRateLimiters  map[string]*rate.Limiter
	requestCount      int64
	lastResetTime     time.Time
	metrics           *Metrics
	isTestnetMode     bool
	maxSyncBatch      int
	currentSyncBatch  int
	batchMutex        sync.Mutex
	quotaMonitor      *monitor.QuotaMonitor // RPC 额度监控器
}

// NewEnhancedRPCClientPool creates an enhanced RPC client pool with testnet-specific configurations
func NewEnhancedRPCClientPool(urls []string, isTestnet bool, maxSyncBatch int) (*EnhancedRPCClientPool, error) {
	return NewEnhancedRPCClientPoolWithTimeout(urls, isTestnet, maxSyncBatch, 10*time.Second)
}

// NewEnhancedRPCClientPoolWithTimeout creates an enhanced RPC client pool with custom timeout
func NewEnhancedRPCClientPoolWithTimeout(urls []string, isTestnet bool, maxSyncBatch int, timeout time.Duration) (*EnhancedRPCClientPool, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided")
	}

	// Determine if we are in a local environment (e.g., Anvil)
	isLocal := false
	for _, url := range urls {
		if strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") || strings.Contains(url, "anvil") {
			isLocal = true
			break
		}
	}

	// Determine rate limits based on network type
	var globalRPS float64
	if isLocal {
		globalRPS = 10000.0 // Virtually unlimited for local nodes
	} else if isTestnet {
		globalRPS = 1.0 // Conservative rate for testnet to preserve quotas
	} else {
		globalRPS = 20.0 // Higher rate for local/anvil
	}

	pool := &EnhancedRPCClientPool{
		clients:           make([]*rpcNode, 0, len(urls)),
		globalRateLimiter: rate.NewLimiter(rate.Limit(globalRPS), int(globalRPS*2)),
		nodeRateLimiters:  make(map[string]*rate.Limiter),
		metrics:           GetMetrics(),
		isTestnetMode:     isTestnet && !isLocal, // Disable testnet restrictions for local
		maxSyncBatch:      maxSyncBatch,
		lastResetTime:     time.Now(),
		quotaMonitor:      monitor.NewQuotaMonitor(),
	}

	// Initialize individual node rate limiters
	for _, url := range urls {
		// Per-node rate limiter
		var nodeRPS float64
		if isLocal {
			nodeRPS = 5000.0
		} else if isTestnet {
			nodeRPS = 1.0
		} else {
			nodeRPS = 10.0
		}

		pool.nodeRateLimiters[url] = rate.NewLimiter(rate.Limit(nodeRPS), int(nodeRPS))

		// Create the actual RPC client
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Printf("Warning: failed to connect to %s: %v", url, err)
			continue
		}

		node := &rpcNode{
			url:       url,
			client:    client,
			isHealthy: false,
			weight:    1, // Default weight
		}

		// First node gets higher weight if in testnet mode
		if isTestnet && len(pool.clients) == 0 {
			node.weight = 3
		}

		// Perform initial health check
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err = client.HeaderByNumber(ctx, nil)
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
		log.Printf("Warning: no RPC nodes connected initially")
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
	log.Printf("Enhanced RPC Pool initialized with %d/%d nodes healthy (testnet_mode: %v)", healthyCount, len(pool.clients), isTestnet)

	return pool, nil
}

// Close closes all client connections
func (p *EnhancedRPCClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, node := range p.clients {
		if node.client != nil {
			node.client.Close()
		}
	}
	log.Printf("Enhanced RPC Pool closed")
}

// getNextHealthyNode gets the next healthy node with weighted selection
func (p *EnhancedRPCClientPool) getNextHealthyNode() *rpcNode {
	p.mu.RLock()
	defer p.mu.RUnlock()

	size := int(p.size)
	if size == 0 {
		return nil
	}

	// Calculate total weight of healthy nodes
	totalWeight := 0
	var healthyNodes []*rpcNode
	for _, node := range p.clients {
		if node.isHealthy || time.Now().After(node.retryAfter) {
			healthyNodes = append(healthyNodes, node)
			// Default weight to 1 if not set
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

	// Simple weighted random or round-robin based on counter
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
		// 5 minute cooldown for 429s
		node.retryAfter = time.Now().Add(5 * time.Minute)
		p.mu.Unlock()

		// Report red light to Prometheus
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

	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s, max 60s
	backoffSec := int(math.Min(math.Pow(2, float64(node.failCount-1)), 60))
	node.retryAfter = node.lastError.Add(time.Duration(backoffSec) * time.Second)

	LogRPCRequestFailed("node_unhealthy", node.url, fmt.Errorf("fail_count: %d, retry_after: %v", node.failCount, node.retryAfter.Format("15:04:05")))
	log.Printf("RPC node %s marked unhealthy (fail count: %d, retry after %ds)", node.url, node.failCount, backoffSec)
}

// incrementRequestCount increments the global request counter
func (p *EnhancedRPCClientPool) incrementRequestCount(nodeURL, method string) {
	atomic.AddInt64(&p.requestCount, 1)

	// 📊 追踪额度使用（每次 RPC 调用前调用）
	if p.quotaMonitor != nil {
		p.quotaMonitor.Inc()
	}

	// Record metric
	if p.metrics != nil {
		duration := time.Since(p.lastResetTime)
		p.metrics.RecordRPCRequest(nodeURL, method, duration, true)
	}
}

// enforceSyncBatchLimit enforces the maximum sync batch size to prevent quota exhaustion
func (p *EnhancedRPCClientPool) enforceSyncBatchLimit() error {
	p.batchMutex.Lock()
	defer p.batchMutex.Unlock()

	if p.isTestnetMode {
		p.currentSyncBatch++

		if p.currentSyncBatch > p.maxSyncBatch {
			// Wait before allowing more requests
			log.Printf("Sync batch limit reached (%d/%d), pausing for 5 seconds", p.currentSyncBatch, p.maxSyncBatch)
			time.Sleep(5 * time.Second)
			p.currentSyncBatch = 0
		}
	}

	return nil
}

// BlockByNumber fetches a block with enhanced rate limiting and sync batch control
func (p *EnhancedRPCClientPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	if p.isTestnetMode {
		// Enforce sync batch limit
		if err := p.enforceSyncBatchLimit(); err != nil {
			return nil, err
		}

		// Wait for global rate limiter token
		if err := p.globalRateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("global rate limiter error: %w", err)
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			log.Printf("⚠️ CRITICAL: All RPC nodes are unhealthy!")
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// Wait for per-node rate limiter token if in testnet mode
		if p.isTestnetMode {
			if err := p.nodeRateLimiters[node.url].Wait(ctx); err != nil {
				return nil, fmt.Errorf("node rate limiter error: %w", err)
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		block, err := node.client.BlockByNumber(reqCtx, number)
		cancel()

		// Increment request counter regardless of success/failure
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
			log.Printf("RPC node %s recovered to healthy", node.url)
		}

		return block, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for BlockByNumber")
}

// HeaderByNumber fetches a block header with enhanced rate limiting
func (p *EnhancedRPCClientPool) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if p.isTestnetMode {
		// Enforce sync batch limit
		if err := p.enforceSyncBatchLimit(); err != nil {
			return nil, err
		}

		// Wait for global rate limiter token
		if err := p.globalRateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("global rate limiter error: %w", err)
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// Wait for per-node rate limiter token if in testnet mode
		if p.isTestnetMode {
			if err := p.nodeRateLimiters[node.url].Wait(ctx); err != nil {
				return nil, fmt.Errorf("node rate limiter error: %w", err)
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		header, err := node.client.HeaderByNumber(reqCtx, number)
		cancel()

		// Increment request counter regardless of success/failure
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
			log.Printf("RPC node %s recovered to healthy", node.url)
		}

		return header, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for HeaderByNumber")
}

// FilterLogs fetches logs with enhanced rate limiting
func (p *EnhancedRPCClientPool) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if p.isTestnetMode {
		// Enforce sync batch limit
		if err := p.enforceSyncBatchLimit(); err != nil {
			return nil, err
		}

		// Wait for global rate limiter token
		if err := p.globalRateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("global rate limiter error: %w", err)
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			log.Printf("⚠️ CRITICAL: All RPC nodes are unhealthy!")
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// Wait for per-node rate limiter token if in testnet mode
		if p.isTestnetMode {
			if err := p.nodeRateLimiters[node.url].Wait(ctx); err != nil {
				return nil, fmt.Errorf("node rate limiter error: %w", err)
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		logs, err := node.client.FilterLogs(reqCtx, q)
		cancel()

		// Increment request counter regardless of success/failure
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
			log.Printf("RPC node %s recovered to healthy", node.url)
		}

		return logs, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for FilterLogs")
}

// GetLatestBlockNumber fetches the latest block number with enhanced rate limiting
func (p *EnhancedRPCClientPool) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	if p.isTestnetMode {
		// Wait for global rate limiter token
		if err := p.globalRateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("global rate limiter error: %w", err)
		}
	}

	for attempts := 0; attempts < int(p.size); attempts++ {
		node := p.getNextHealthyNode()
		if node == nil {
			log.Printf("⚠️ CRITICAL: All RPC nodes are unhealthy!")
			return nil, fmt.Errorf("no healthy RPC nodes available")
		}

		// Wait for per-node rate limiter token if in testnet mode
		if p.isTestnetMode {
			if err := p.nodeRateLimiters[node.url].Wait(ctx); err != nil {
				return nil, fmt.Errorf("node rate limiter error: %w", err)
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		header, err := node.client.HeaderByNumber(reqCtx, nil)
		cancel()

		// Increment request counter regardless of success/failure
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
			log.Printf("RPC node %s recovered to healthy", node.url)
		}

		return header.Number, nil
	}

	return nil, fmt.Errorf("all RPC nodes failed for GetLatestBlockNumber")
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

// IsTestnetMode returns whether the pool is in testnet mode
func (p *EnhancedRPCClientPool) IsTestnetMode() bool {
	return p.isTestnetMode
}

// GetHealthyNodeCount returns the number of healthy nodes
func (p *EnhancedRPCClientPool) GetHealthyNodeCount() int {
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

// GetTotalNodeCount returns the total number of nodes
func (p *EnhancedRPCClientPool) GetTotalNodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

// SetRateLimit updates the global and per-node rate limits
func (p *EnhancedRPCClientPool) SetRateLimit(rps float64, burst int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.globalRateLimiter = rate.NewLimiter(rate.Limit(rps), burst)

	// Also update individual node limiters (using rps/num_nodes or just rps)
	// For simplicity, we give each node the same burst, but global handles the total
	for url := range p.nodeRateLimiters {
		p.nodeRateLimiters[url] = rate.NewLimiter(rate.Limit(rps), burst)
	}

	log.Printf("Enhanced RPC Pool rate limit updated: %.2f RPS, %d Burst", rps, burst)
}

// StartHealthCheck starts a background goroutine to periodically check the health of RPC nodes.
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

// checkHealth checks all nodes' health status and updates Prometheus metrics
func (p *EnhancedRPCClientPool) checkHealth() {
	p.mu.Lock()
	defer p.mu.Unlock()

	healthyNodes := 0
	for _, node := range p.clients {
		if node.isHealthy {
			// Perform simple health check
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
		} else {
			// Try to recover unhealthy nodes after 30 seconds
			if time.Since(node.lastError) > 30*time.Second {
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
	}

	// Report to Prometheus
	GetMetrics().UpdateRPCHealthyNodes("enhanced", healthyNodes)
}
