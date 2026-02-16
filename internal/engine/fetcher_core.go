package engine

import (
	"context"
	"log/slog"
	"math/big"
	"sync"

	"web3-indexer-go/internal/limiter"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/time/rate"
)

type BlockData struct {
	Number   *big.Int
	RangeEnd *big.Int // Used for range processing (if applicable)
	Block    *types.Block
	Err      error
	Logs     []types.Log
}

type FetchJob struct {
	Start *big.Int
	End   *big.Int
}

type Fetcher struct {
	pool        RPCClient // RPC客户端接口，支持Mock和真实实现
	concurrency int
	jobs        chan FetchJob
	Results     chan BlockData
	limiter     *rate.Limiter // 速率限制器
	stopCh      chan struct{} // 用于停止调度
	stopOnce    sync.Once     // 确保只停止一次
	metrics     *Metrics      // Prometheus metrics

	// Pause/Resume 机制：用 sync.Cond 替代 channel 避免竞态
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    bool

	// Watched addresses for contract monitoring
	watchedAddresses []common.Address

	headerOnlyMode bool // 低成本模式：仅获取区块头，不获取Logs
}

// SetHeaderOnlyMode enables/disables low-cost mode
func (f *Fetcher) SetHeaderOnlyMode(enabled bool) {
	f.headerOnlyMode = enabled
	if enabled {
		Logger.Info("fetcher_switched_to_low_cost_header_only_mode")
	} else {
		Logger.Info("fetcher_switched_to_full_data_mode")
	}
}

func NewFetcher(pool RPCClient, concurrency int) *Fetcher {
	// 彻底关闭限速
	limiter := rate.NewLimiter(rate.Inf, 0)

	f := &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan FetchJob, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     limiter,
		stopCh:      make(chan struct{}),
		paused:      false,
		metrics:     GetMetrics(),
	}
	f.pauseCond = sync.NewCond(&f.pauseMu)
	return f
}

func NewFetcherWithLimiter(pool RPCClient, concurrency, rps, burst int) *Fetcher {
	// ✨ 使用工业级限流器（自动降级保护）
	rateLimiter := limiter.NewRateLimiter(rps)
	if burst > 0 {
		rateLimiter.Limiter().SetBurst(burst)
	}

	slog.Info("🛡️ Rate limiter initialized",
		"max_rps", rateLimiter.MaxRPS(),
		"concurrency", concurrency,
		"protection", "industrial_grade")

	f := &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan FetchJob, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     rateLimiter.Limiter(), // 使用工业级限流器内部的 limiter
		stopCh:      make(chan struct{}),
		paused:      false,
		metrics:     GetMetrics(),
	}
	f.pauseCond = sync.NewCond(&f.pauseMu)
	return f
}

// SetWatchedAddresses sets the contract addresses to monitor for Transfer events
func (f *Fetcher) SetWatchedAddresses(addresses []string) {
	f.watchedAddresses = make([]common.Address, 0, len(addresses))
	for _, addr := range addresses {
		if addr != "" {
			f.watchedAddresses = append(f.watchedAddresses, common.HexToAddress(addr))
		}
	}
}

func (f *Fetcher) Start(ctx context.Context, wg *sync.WaitGroup) {
	Logger.Info("📢 [Fetcher] 引擎协程已进入 Start 函数！",
		slog.Int("concurrency", f.concurrency),
	)
	for i := 0; i < f.concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			Logger.Info("🌀 [Fetcher] 循环抓取协程正式启动...",
				slog.Int("worker_id", workerID),
			)
			f.worker(ctx, wg)
		}(i)
	}
}

func (f *Fetcher) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.stopCh:
			return
		case job, ok := <-f.jobs:
			if !ok {
				return
			}

			// 检查是否暂停（Reorg 处理期间）
			f.pauseMu.Lock()
			for f.paused {
				// 等待恢复信号（使用 Cond.Wait 避免竞态）
				f.pauseCond.Wait()
			}
			f.pauseMu.Unlock()

			// 检查是否已停止（在 unlock 后再检查）
			select {
			case <-ctx.Done():
				return
			case <-f.stopCh:
				return
			default:
			}

			// 等待速率限制令牌
			if err := f.limiter.Wait(ctx); err != nil {
				select {
				case f.Results <- BlockData{Number: job.Start, RangeEnd: job.End, Err: err}:
				case <-ctx.Done():
					return
				case <-f.stopCh:
					return
				}
				continue
			}

			// 获取范围区块数据
			f.fetchRangeWithLogs(ctx, job.Start, job.End)
		}
	}
}
