package engine

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
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
	pauseCh     chan struct{} // 用于暂停 worker
	resumeCh    chan struct{} // 用于恢复 worker
	paused      atomic.Bool   // 当前是否暂停
}

func NewFetcher(pool *RPCClientPool, concurrency int) *Fetcher {
	// 默认限制：每秒100个请求，突发200
	limiter := rate.NewLimiter(rate.Limit(100), 200)
	
	return &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan *big.Int, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     limiter,
		stopCh:      make(chan struct{}),
		pauseCh:     make(chan struct{}),
		resumeCh:    make(chan struct{}),
	}
}

func NewFetcherWithLimiter(pool *RPCClientPool, concurrency int, rps int, burst int) *Fetcher {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	
	return &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan *big.Int, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     limiter,
		stopCh:      make(chan struct{}),
		pauseCh:     make(chan struct{}),
		resumeCh:    make(chan struct{}),
	}
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
			if f.paused.Load() {
				select {
				case <-f.resumeCh:
					// 恢复后继续
				case <-ctx.Done():
					return
				case <-f.stopCh:
					return
				}
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
	
	// 指数退避重试逻辑 (RPC pool 内部有节点故障转移)
	for retries := 0; retries < 3; retries++ {
		block, err = f.pool.BlockByNumber(ctx, bn)
		if err == nil {
			break
		}
		
		// 指数退避：100ms, 200ms, 400ms
		backoff := time.Duration(100*(1<<retries)) * time.Millisecond
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
	
	return block, logs, nil
}

func (f *Fetcher) Schedule(start, end *big.Int) {
	go func() {
		for i := new(big.Int).Set(start); i.Cmp(end) <= 0; i.Add(i, big.NewInt(1)) {
			select {
			case <-f.stopCh:
				return
			default:
				bn := new(big.Int).Set(i)
				f.jobs <- bn
			}
		}
		close(f.jobs)
	}()
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
	if f.paused.CompareAndSwap(false, true) {
		LogFetcherPaused("reorg_handling")
	}
}

// Resume 恢复 Fetcher
func (f *Fetcher) Resume() {
	if f.paused.CompareAndSwap(true, false) {
		close(f.resumeCh)
		f.resumeCh = make(chan struct{}) // 重新初始化以备下次使用
		LogFetcherResumed()
	}
}

// IsPaused 返回当前是否暂停
func (f *Fetcher) IsPaused() bool {
	return f.paused.Load()
}
func (f *Fetcher) SetRateLimit(rps int, burst int) {
	f.limiter.SetLimit(rate.Limit(rps))
	f.limiter.SetBurst(burst)
}
