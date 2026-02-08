package engine

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/time/rate"
)

type BlockData struct {
	Block *types.Block
	Logs  []types.Log
	Err   error
}

type Fetcher struct {
	pool        *RPCClientPool // 多节点RPC池
	concurrency int
	jobs        chan *big.Int
	Results     chan BlockData
	limiter     *rate.Limiter // 速率限制器
	stopCh      chan struct{} // 用于停止调度
	stopOnce    sync.Once     // 确保只停止一次
	
	// Pause/Resume 机制：用 sync.Cond 替代 channel 避免竞态
	pauseMu     sync.Mutex
	pauseCond   *sync.Cond
	paused      bool
}

func NewFetcher(pool *RPCClientPool, concurrency int) *Fetcher {
	// 默认限制：每秒100个请求，突发200
	limiter := rate.NewLimiter(rate.Limit(100), 200)
	
	f := &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan *big.Int, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     limiter,
		stopCh:      make(chan struct{}),
		paused:      false,
	}
	f.pauseCond = sync.NewCond(&f.pauseMu)
	return f
}

func NewFetcherWithLimiter(pool *RPCClientPool, concurrency int, rps int, burst int) *Fetcher {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	
	f := &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan *big.Int, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     limiter,
		stopCh:      make(chan struct{}),
		paused:      false,
	}
	f.pauseCond = sync.NewCond(&f.pauseMu)
	return f
}

func (f *Fetcher) Start(ctx context.Context, wg *sync.WaitGroup) {
	for i := 0; i < f.concurrency; i++ {
		wg.Add(1)
		go f.worker(ctx, wg)
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
		case bn, ok := <-f.jobs:
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
				case f.Results <- BlockData{Err: err}:
				case <-ctx.Done():
					return
				case <-f.stopCh:
					return
				}
				continue
			}
			
			// 获取区块数据
			block, logs, err := f.fetchBlockWithLogs(ctx, bn)
			
			select {
			case f.Results <- BlockData{Block: block, Logs: logs, Err: err}:
			case <-ctx.Done():
				return
			case <-f.stopCh:
				return
			}
		}
	}
}

func (f *Fetcher) fetchBlockWithLogs(ctx context.Context, bn *big.Int) (*types.Block, []types.Log, error) {
	var block *types.Block
	var err error
	start := time.Now()
	
	// 指数退避重试逻辑 (RPC pool 内部有节点故障转移)
	for retries := 0; retries < 3; retries++ {
		block, err = f.pool.BlockByNumber(ctx, bn)
		if err == nil {
			break
		}
		
		// 根据错误类型选择退避时间
		// 429 (Too Many Requests) 需要更长的退避
		var backoff time.Duration
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "too many requests") {
			// 429 错误：1s, 2s, 4s（更长的退避）
			backoff = time.Duration(1000*(1<<retries)) * time.Millisecond
		} else {
			// 其他错误：100ms, 200ms, 400ms
			backoff = time.Duration(100*(1<<retries)) * time.Millisecond
		}
		
		LogRPCRetry("BlockByNumber", retries+1, err)
		select {
		case <-time.After(backoff):
			// 继续重试
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-f.stopCh:
			return nil, nil, fmt.Errorf("fetcher stopped")
		}
	}
	
	if err != nil {
		return nil, nil, err
	}
	
	// 获取该区块的日志（Transfer事件）
	logs, err := f.pool.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: bn,
		ToBlock:   bn,
		Topics:    [][]common.Hash{{TransferEventHash}},
	})
	if err != nil {
		// 日志获取失败不阻塞区块处理
		logs = []types.Log{}
	}
	
	// 记录 fetch 耗时
	GetMetrics().RecordFetcherJobCompleted(time.Since(start))
	
	return block, logs, nil
}

func (f *Fetcher) Schedule(ctx context.Context, start, end *big.Int) error {
	for i := new(big.Int).Set(start); i.Cmp(end) <= 0; i.Add(i, big.NewInt(1)) {
		bn := new(big.Int).Set(i)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.stopCh:
			return fmt.Errorf("fetcher stopped")
		case f.jobs <- bn:
		}
	}
	return nil
}

// Stop 优雅地停止 Fetcher，清空任务队列
func (f *Fetcher) Stop() {
	f.stopOnce.Do(func() {
		close(f.stopCh)
		// 清空 jobs channel 防止阻塞
		go func() {
			for range f.jobs {
			}
		}()
	})
}

// Pause 暂停 Fetcher（用于 Reorg 处理期间防止写入旧分叉数据）
func (f *Fetcher) Pause() {
	f.pauseMu.Lock()
	defer f.pauseMu.Unlock()
	if !f.paused {
		f.paused = true
		LogFetcherPaused("reorg_handling")
	}
}

// Resume 恢复 Fetcher
func (f *Fetcher) Resume() {
	f.pauseMu.Lock()
	defer f.pauseMu.Unlock()
	if f.paused {
		f.paused = false
		f.pauseCond.Broadcast() // 唤醒所有等待的 worker
		LogFetcherResumed()
	}
}

// IsPaused 返回当前是否暂停
func (f *Fetcher) IsPaused() bool {
	f.pauseMu.Lock()
	defer f.pauseMu.Unlock()
	return f.paused
}
func (f *Fetcher) SetRateLimit(rps int, burst int) {
	f.limiter.SetLimit(rate.Limit(rps))
	f.limiter.SetBurst(burst)
}
