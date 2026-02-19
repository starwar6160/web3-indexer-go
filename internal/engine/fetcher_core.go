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
	throughput  *rate.Limiter // 🚀 Throughput limiter for visual/speed control
	bpsLimiter  *rate.Limiter // 🚀 🔥 新增：块级别节拍器 (Pacemaker)
	stopCh      chan struct{} // 用于停止调度
	stopOnce    sync.Once     // 确保只停止一次
	metrics     *Metrics      // Prometheus metrics

	// Pause/Resume 机制：用 sync.Cond 替代 channel 避免竞态
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    bool

	// Watched addresses for contract monitoring
	watchedAddresses []common.Address

	headerOnlyMode bool          // 低成本模式：仅获取区块头，不获取Logs
	recorder       *DataRecorder // 💾 原始数据录制器

	// 🔥 横滨实验室：背压检测
	sequencer *Sequencer // Sequencer 引用（用于检测 buffer 深度）
}

// 🔥 QueueDepth 返回队列深度（用于上游背压检测）
func (f *Fetcher) QueueDepth() int {
	return len(f.jobs)
}

// 🔥 ResultsDepth 返回结果通道深度（用于上游背压检测）
func (f *Fetcher) ResultsDepth() int {
	return len(f.Results)
}

// 🔥 SetSequencer 设置 Sequencer 引用（用于背压检测）
func (f *Fetcher) SetSequencer(seq *Sequencer) {
	f.sequencer = seq
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

	// 💾 初始化录制器
	recorder, err := NewDataRecorder("")
	if err != nil {
		slog.Warn("failed_to_initialize_recorder", "err", err)
	}

	f := &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		// 🔥 横滨实验室：Jobs channel 也扩容 (concurrency * 10)
		jobs: make(chan FetchJob, concurrency*10),
		// 🔥 16G RAM 调优：提升至 15,000，给予消费端更多缓冲空间
		Results:  make(chan BlockData, 15000),
		limiter:  limiter,
		recorder: recorder,
		stopCh:   make(chan struct{}),
		paused:   false,
		metrics:  GetMetrics(),
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

		// 🚀 Hard Throttle: Limit ingestion to 2.0 TPS to protect remaining quota

		throughput := rate.NewLimiter(rate.Limit(2.0), 1000)

	

		// 🚀 Pacemaker: 每秒最多允许处理 200 个块，确保 UI 数字匀速跳动

		bpsLimiter := rate.NewLimiter(rate.Limit(200.0), 50)

	

		// 💾 初始化录制器 (默认存储路径)

		recorder, err := NewDataRecorder("")

		if err != nil {

			slog.Warn("failed_to_initialize_recorder", "err", err)

		}

	

		// 🔥 16G RAM 调优：提升至 15,000

		f := &Fetcher{

			pool:         pool,

			concurrency:  concurrency,

			jobs:         make(chan FetchJob, concurrency*10), // 扩容 10 倍

			Results:      make(chan BlockData, 15000),       // 16G RAM 环境适中配置

			limiter:      rateLimiter.Limiter(),

			throughput:   throughput,

			bpsLimiter:   bpsLimiter,

			recorder:     recorder,

			stopCh:       make(chan struct{}),

			paused:       false,

			metrics:      GetMetrics(),

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

// SetThroughputLimit updates the target processing speed
func (f *Fetcher) SetThroughputLimit(tps float64) {
	if tps <= 0 {
		f.throughput = rate.NewLimiter(rate.Inf, 0)
		return
	}
	// 🚀 允许 1000 的 Burst，这样即便大块也能进入队列，但消耗令牌会产生后续延迟
	f.throughput = rate.NewLimiter(rate.Limit(tps), 1000)
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

			// 🚀 🔥 新增：工作脉搏，帮助定位为何 Jobs 堵塞
			slog.Debug("🌀 [Fetcher] Worker picking up job", "start", job.Start.String(), "end", job.End.String())
			GetOrchestrator().DispatchLog("DEBUG", "🌀 Fetcher: Worker processing job", "start", job.Start.String())

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
