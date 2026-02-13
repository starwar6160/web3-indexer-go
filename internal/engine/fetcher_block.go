package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

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

	// 低成本模式优化：跳过日志获取
	if f.headerOnlyMode {
		return block, []types.Log{}, nil
	}

	// 获取该区块的日志（Transfer事件）
	// 如果有监听的地址，只获取这些地址的日志；否则获取所有Transfer事件
	filterQuery := ethereum.FilterQuery{
		FromBlock: bn,
		ToBlock:   bn,
		Topics:    [][]common.Hash{{TransferEventHash}},
	}

	if len(f.watchedAddresses) > 0 {
		filterQuery.Addresses = f.watchedAddresses
		Logger.Debug("fetcher_filtering_logs",
			slog.String("block", bn.String()),
			slog.Int("watched_addresses_count", len(f.watchedAddresses)),
		)
	}

	logs, err := f.pool.FilterLogs(ctx, filterQuery)

	Logger.Debug("🌐 RPC：执行 eth_getLogs",
		slog.String("stage", "FETCHER"),
		slog.String("block", bn.String()),
		slog.Int("logs_returned", len(logs)),
		slog.Int("watched_addresses_count", len(f.watchedAddresses)),
	)

	if err != nil {
		// 日志获取失败不阻塞区块处理，但记录详细错误信息
		Logger.Warn("logs_fetch_failed",
			slog.String("block_number", bn.String()),
			slog.String("error", err.Error()),
			slog.String("action", "continuing_with_empty_logs"),
		)
		logs = []types.Log{}
	}

	// 记录 fetch 耗时
	GetMetrics().RecordFetcherJobCompleted(time.Since(start))

	return block, logs, nil
}