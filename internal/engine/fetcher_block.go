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

	// Step 3: Fetch Full Blocks (with transactions) for blocks that have logs
	for bNum, blockLogs := range logsByBlock {
		bn := new(big.Int).SetUint64(bNum)
		
		// 🚀 修复：使用 BlockByNumber 获取完整区块（包含交易），而不是只用 Header
		block, err := f.pool.BlockByNumber(ctx, bn)
		if err != nil {
			Logger.Warn("⚠️ [FETCHER] Failed to fetch full block",
				"block", bn,
				"err", err)
			f.sendResult(ctx, BlockData{Number: bn, Err: err})
			continue
		}

		// 🚀 防御性检查：确保 block 不为 nil
		if block == nil {
			slog.Warn("⚠️ [FETCHER] Received nil block for block with logs",
				"block", bn,
				"skip", true)
			continue
		}

		Logger.Debug("📡 [FETCHER_RAW_CHECK]",
			slog.String("block", bn.String()),
			slog.Int("tx_count", block.Transactions().Len()),
			slog.Uint64("gas_used", block.GasUsed()))

		f.sendResult(ctx, BlockData{Number: bn, Block: block, Logs: blockLogs})
	}

	// Step 4: Full Range Reporting (Keep-alive)
	// We MUST report every block in the range to the Sequencer to prevent gaps.
	// For blocks without logs, we send a minimal BlockData with just the number.
	for i := new(big.Int).Set(start); i.Cmp(end) <= 0; i.Add(i, big.NewInt(1)) {
		bn := new(big.Int).Set(i)
		if _, exists := logsByBlock[bn.Uint64()]; exists {
			continue // Already sent in Step 3
		}

	// Fetch full block for the very last block in range to update UI time and tx count
		// For others, we can be lazy and send nil Block to just move the pointer
		var block *types.Block
		if bn.Cmp(end) == 0 {
			// 🚀 修复：使用 BlockByNumber 获取完整区块（包含交易）
			var err error
			block, err = f.pool.BlockByNumber(ctx, bn)
			if err != nil {
				slog.Warn("⚠️ [FETCHER] Failed to fetch full block for last block",
					"block", bn,
					"err", err,
					"skip", false) // 继续发送，但 block 为 nil
			}
			
			// 🚀 防御性：如果 fetch 失败，仍然发送但 block 为 nil
			if block == nil {
				slog.Warn("⚠️ [FETCHER] Sending nil block for last block",
					"block", bn,
					"skip", false)
			}
		}

		f.sendResult(ctx, BlockData{
			Number:   bn,
			RangeEnd: end, // Pass range end for checkpointing
			Block:    block,
			Logs:     []types.Log{},
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
