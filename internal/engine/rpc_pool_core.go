package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/monitor"

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

// NewEnhancedRPCClientPool creates an enhanced RPC client pool
func NewEnhancedRPCClientPool(urls []string, isTestnet bool, maxSyncBatch int) (*EnhancedRPCClientPool, error) {
	return NewEnhancedRPCClientPoolWithTimeout(urls, isTestnet, maxSyncBatch, 10*time.Second)
}

// NewEnhancedRPCClientPoolWithTimeout creates an enhanced RPC client pool with custom timeout
func NewEnhancedRPCClientPoolWithTimeout(urls []string, isTestnet bool, maxSyncBatch int, timeout time.Duration) (*EnhancedRPCClientPool, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided")
	}

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
		globalRPS = 500.0
	} else if isTestnet && !forceRPS {
		globalRPS = 15.0
	} else {
		globalRPS = 20.0
	}

	pool := &EnhancedRPCClientPool{
		clients:           make([]*rpcNode, 0, len(urls)),
		globalRateLimiter: rate.NewLimiter(rate.Limit(globalRPS), int(globalRPS*2)),
		nodeRateLimiters:  make(map[string]*rate.Limiter),
		metrics:           GetMetrics(),
		isTestnetMode:     isTestnet && !isLocal,
		maxSyncBatch:      maxSyncBatch,
		lastResetTime:     time.Now(),
		quotaMonitor:      monitor.NewQuotaMonitor(),
		rpcURLs:           urls,
	}

	for _, url := range urls {
		pool.nodeRateLimiters[url] = rate.NewLimiter(rate.Limit(globalRPS), int(globalRPS*2))
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Printf("Warning: failed to connect to %s: %v", url, err)
			continue
		}

		node := &rpcNode{
			url:       url,
			client:    client,
			isHealthy: false,
			weight:    1,
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err = client.HeaderByNumber(ctx, nil)
		cancel()

		node.isHealthy = (err == nil)
		pool.clients = append(pool.clients, node)
	}

	// #nosec G115 - Number of RPC nodes is very small
	pool.size = int32(len(pool.clients))
	return pool, nil
}

// NewRPCClientPool creates a basic RPC pool
func NewRPCClientPool(urls []string) (*RPCClientPool, error) {
	return NewRPCClientPoolWithTimeout(urls, 10*time.Second)
}

// NewRPCClientPoolWithTimeout creates a basic RPC pool with custom timeout
func NewRPCClientPoolWithTimeout(urls []string, _ time.Duration) (*RPCClientPool, error) {
	pool := &RPCClientPool{
		clients: make([]*rpcNode, 0, len(urls)),
	}

	for _, url := range urls {
		client, err := ethclient.Dial(url)
		if err != nil {
			continue
		}
		node := &rpcNode{
			url:       url,
			client:    client,
			isHealthy: true,
		}
		pool.clients = append(pool.clients, node)
	}

	// #nosec G115 - Number of RPC nodes is very small
	pool.size = int32(len(pool.clients))
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
}

// SetRateLimit updates the global and per-node rate limits
func (p *EnhancedRPCClientPool) SetRateLimit(rps float64, burst int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.globalRateLimiter = rate.NewLimiter(rate.Limit(rps), burst)
	for _, url := range p.rpcURLs {
		p.nodeRateLimiters[url] = rate.NewLimiter(rate.Limit(rps), burst)
	}
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
