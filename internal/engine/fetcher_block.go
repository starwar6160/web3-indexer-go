package engine

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// fetchRangeWithLogs fetches logs for a range of blocks and processes them.
// If no logs are found, it fetches the header of the latest block in range to update progress.
func (f *Fetcher) fetchRangeWithLogs(ctx context.Context, start, end *big.Int) {
	startTime := time.Now()

	// Step 1: Range Filter
	filterQuery := ethereum.FilterQuery{
		FromBlock: start,
		ToBlock:   end,
	}

	if len(f.watchedAddresses) > 0 {
		filterQuery.Addresses = f.watchedAddresses
		// For specific addresses, we still filter by Transfer event to save RPC weight
		filterQuery.Topics = [][]common.Hash{{TransferEventHash}}
		Logger.Info("🔍 Fetching logs with address filter",
			slog.String("from", start.String()),
			slog.String("to", end.String()),
			slog.Int("watched_count", len(f.watchedAddresses)))
	} else {
		// 🚀 Industrial Grade: Unfiltered mode captures EVERYTHING
		// No Topics = No Filter = All contract events captured
		filterQuery.Topics = nil
		Logger.Info("🌍 Fetching logs for ALL events (Full Sniffing)",
			slog.String("from", start.String()),
			slog.String("to", end.String()))
	}

	logs, err := f.pool.FilterLogs(ctx, filterQuery)
	if err != nil {
		// Log error and send results back
		select {
		case f.Results <- BlockData{Number: start, RangeEnd: end, Err: err}:
		case <-ctx.Done():
		case <-f.stopCh:
		}
		return
	}

	Logger.Info("📊 RPC response received",
		slog.String("from", start.String()),
		slog.String("to", end.String()),
		slog.Int("logs_found", len(logs)),
		slog.Int("watched_addresses", len(f.watchedAddresses)))

	// Step 2: Group logs by block number
	logsByBlock := make(map[uint64][]types.Log)
	for _, vLog := range logs {
		logsByBlock[vLog.BlockNumber] = append(logsByBlock[vLog.BlockNumber], vLog)
	}

	// Step 3 & 4: Sequential Reporting (The Serpentine Ingestion)
	// We MUST report every block in chronological order to prevent Sequencer bursts.
	for i := new(big.Int).Set(start); i.Cmp(end) <= 0; i.Add(i, big.NewInt(1)) {
		bn := new(big.Int).Set(i)
		blockLogs := logsByBlock[bn.Uint64()]

		var block *types.Block
		var err error

		// Only fetch full block if it has logs or it's the range end (to update UI time)
		if len(blockLogs) > 0 || bn.Cmp(end) == 0 {
			block, err = f.pool.BlockByNumber(ctx, bn)
			if err != nil {
				slog.Warn("⚠️ [FETCHER] Block fetch failed", "block", bn, "err", err)
			}
		}

		f.sendResult(ctx, BlockData{
			Number:   bn,
			RangeEnd: end,
			Block:    block,
			Logs:     blockLogs,
			Err:      err,
		})
	}

	if f.metrics != nil {
		f.metrics.RecordFetcherJobCompleted(time.Since(startTime))
	}
}

func (f *Fetcher) fetchHeaderWithRetry(ctx context.Context, bn *big.Int) (*types.Header, error) {
	var header *types.Header
	var err error

	for retries := 0; retries < 3; retries++ {
		header, err = f.pool.HeaderByNumber(ctx, bn)
		if err == nil {
			return header, nil
		}

		backoff := time.Duration(100*(1<<uint(retries))) * time.Millisecond
		if strings.Contains(err.Error(), "429") {
			backoff = time.Duration(1000*(1<<uint(retries))) * time.Millisecond
		}

		select {
		case <-time.After(backoff):
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, err
}

func (f *Fetcher) sendResult(ctx context.Context, data BlockData) {
	// 🚀 工业级节流：基于『交易笔数』进行硬限速
	// 这确保了如果一个块有 500 笔交易，它会强制分摊时间，绝对保住 2.0 TPS
	if f.throughput != nil {
		tokens := len(data.Logs)
		if tokens == 0 {
			tokens = 1 // 即使空块也消耗 1 令牌，维持 2.0 BPS 的心跳
		}
		
		if err := f.throughput.WaitN(ctx, tokens); err != nil {
			return
		}
	}

	select {
	case f.Results <- data:
	case <-ctx.Done():
	case <-f.stopCh:
	}
}

// Deprecated: used for single block fetching, replaced by fetchRangeWithLogs
func (f *Fetcher) fetchBlockWithLogs(ctx context.Context, bn *big.Int) (*types.Block, []types.Log, error) {
	// For compatibility if still called somewhere
	header, err := f.fetchHeaderWithRetry(ctx, bn)
	if err != nil {
		return nil, nil, err
	}

	filterQuery := ethereum.FilterQuery{
		FromBlock: bn,
		ToBlock:   bn,
		Topics:    [][]common.Hash{{TransferEventHash}},
	}
	if len(f.watchedAddresses) > 0 {
		filterQuery.Addresses = f.watchedAddresses
	}
	logs, err := f.pool.FilterLogs(ctx, filterQuery)
	if err != nil {
		return types.NewBlockWithHeader(header), []types.Log{}, nil
	}

	return types.NewBlockWithHeader(header), logs, nil
}
