package engine

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type BlockData struct {
	Block *types.Block
	Logs  []types.Log // 这里简化，实际应根据 Logs 过滤 ERC20
	Err   error
}

type Fetcher struct {
	client      *ethclient.Client
	concurrency int
	jobs        chan *big.Int
	Results     chan BlockData // 输出通道
}

func NewFetcher(client *ethclient.Client, concurrency int) *Fetcher {
	return &Fetcher{
		client:      client,
		concurrency: concurrency,
		jobs:        make(chan *big.Int, concurrency*2), // 缓冲通道
		Results:     make(chan BlockData, concurrency*2),
	}
}

func (f *Fetcher) Start(ctx context.Context, wg *sync.WaitGroup) {
	// 启动 Workers
	for i := 0; i < f.concurrency; i++ {
		wg.Add(1)
		go f.worker(ctx, wg)
	}
}

func (f *Fetcher) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for bn := range f.jobs {
		// 简单的重试逻辑 (Exponential Backoff 可在此处实现)
		var block *types.Block
		var err error
		
		for retries := 0; retries < 3; retries++ {
			block, err = f.client.BlockByNumber(ctx, bn)
			if err == nil {
				break
			}
			time.Sleep(time.Duration(retries+1) * 100 * time.Millisecond)
		}

		// 发送结果（注意：这里不应阻塞太久）
		select {
		case f.Results <- BlockData{Block: block, Err: err}:
		case <-ctx.Done():
			return
		}
	}
}

func (f *Fetcher) Schedule(start, end *big.Int) {
	go func() {
		for i := new(big.Int).Set(start); i.Cmp(end) <= 0; i.Add(i, big.NewInt(1)) {
			// 必须复制 BigInt，否则 goroutine 会共享同一个指针
			bn := new(big.Int).Set(i)
			f.jobs <- bn
		}
		close(f.jobs)
	}()
}
