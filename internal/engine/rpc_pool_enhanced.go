package engine

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"web3-indexer-go/internal/config"
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
	rpcURLs           []string              // Store URLs for RPS calculation
	cfg               *config.Config        // Config for RPS calculation
}

// CalculateOptimalRPS 根据环境自动计算最优 RPS
// 决策优先级：内部安全策略（URL + 高度） > 外部配置（环境变量） > 系统默认值
func CalculateOptimalRPS(rpcURL string, currentLag int64, userConfigRPS int) float64 {
	var policyRPS float64

	// 1. 基于 URL 的硬核判定（最优先）
	isLocal := strings.Contains(rpcURL, "localhost") ||
		strings.Contains(rpcURL, "127.0.0.1")

	if isLocal {
		policyRPS = 500.0 // 本地 Anvil 开启无限火力
	} else if strings.Contains(rpcURL, "infura.io") ||
		strings.Contains(rpcURL, "quiknode.pro") {
		policyRPS = 15.0 // 商业节点安全水位
	} else if strings.Contains(rpcURL, "public.com") {
		policyRPS = 5.0 // 公共节点保守策略
	} else {
		policyRPS = 10.0 // 自定义节点默认策略
	}

	// 2. 基于高度落后的动态修正
	// 如果落后太多，允许在安全范围内加速
	if currentLag > 1000 && !isLocal {
		policyRPS *= 2.0 // 追赶模式翻倍
		log.Printf("🚀 Catch-up mode activated: lag=%d, boosted_rps=%.2f", currentLag, policyRPS)
	}

	// 3. 外部参数作为次级参考
	if userConfigRPS > 0 {
		configured := float64(userConfigRPS)
		if isLocal {
			// 本地模式完全信任用户输入
			log.Printf("🔓 Local mode: unlimited user config (rps=%.2f)", configured)
			return configured
		}
		// 生产环境下，取政策上限和用户设置的最小值
		result := math.Min(policyRPS, configured)
		log.Printf("🛡️ Production mode: user_rps=%.2f, policy_limit=%.2f, final_rps=%.2f",
			configured, policyRPS, result)
		return result
	}

	log.Printf("🎯 Using policy-based RPS: %.2f (type=%s)", policyRPS,
		map[bool]string{true: "local", false: "remote"}[isLocal])
	return policyRPS
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

	// 🧠 智能计算最优 RPS（首次调用时 lag=0，使用默认配置）
	// 注意：这里暂时使用默认值，后续会在 main() 中通过 SetRateLimit 调整
	isLocal := false
	for _, url := range urls {
		if strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") || strings.Contains(url, "anvil") {
			isLocal = true
			break
		}
	}

	const envTrue = "true"
	var globalRPS float64
	forceRPS := os.Getenv("FORCE_RPS") == envTrue

	if isLocal {
		globalRPS = 500.0 // 本地 Anvil 开启火力
	} else if isTestnet && !forceRPS {
		globalRPS = 15.0 // 测试网商业节点安全水位
	} else {
		globalRPS = 20.0 // 主网或强制模式
	}

	log.Printf("🧠 Smart Rate Limiter initialized: %.2f RPS (local=%v, testnet=%v)",
		globalRPS, isLocal, isTestnet)

	pool := &EnhancedRPCClientPool{
		clients:           make([]*rpcNode, 0, len(urls)),
		globalRateLimiter: rate.NewLimiter(rate.Limit(globalRPS), int(globalRPS*2)),
		nodeRateLimiters:  make(map[string]*rate.Limiter),
		metrics:           GetMetrics(),
		isTestnetMode:     isTestnet && !isLocal, // Disable testnet restrictions for local
		maxSyncBatch:      maxSyncBatch,
		lastResetTime:     time.Now(),
		quotaMonitor:      monitor.NewQuotaMonitor(),
		rpcURLs:           urls, // Store URLs for RPS calculation
	}

	// Initialize individual node rate limiters
	for _, url := range urls {
		// Per-node rate limiter
		var nodeRPS float64
		if isLocal {
			nodeRPS = 5000.0
		} else if isTestnet && !forceRPS {
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

	// #nosec G115 - Number of RPC nodes is very small
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

		// 🚀 工业级优化：提高批次上限并缩短惩罚停顿，使 TPS 更加平滑
		if p.currentSyncBatch > 50 {
			// Wait before allowing more requests
			log.Printf("Sync batch threshold reached (%d), micro-pause for 200ms to smooth throughput", p.currentSyncBatch)
			time.Sleep(200 * time.Millisecond)
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
		} else if time.Since(node.lastError) > 30*time.Second {
			// Try to recover unhealthy nodes after 30 seconds
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

	// Report to Prometheus
	GetMetrics().UpdateRPCHealthyNodes("enhanced", healthyNodes)
}

// CallContract fetches contract results with rate limiting
func (p *EnhancedRPCClientPool) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if p.isTestnetMode {
		if err := p.globalRateLimiter.Wait(ctx); err != nil {
			return nil, err
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

// GetClientForMetadata 返回一个用于元数据抓取的客户端（不带限流）
func (p *EnhancedRPCClientPool) GetClientForMetadata() LowLevelRPCClient {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.clients) == 0 {
		return nil
	}

	// 返回第一个健康节点的客户端
	for _, node := range p.clients {
		if node.isHealthy {
			return node.client
		}
	}

	// 如果都不健康，返回第一个
	return p.clients[0].client
}
