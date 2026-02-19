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

// fetchRangeWithLogs fetches logs for a range of blocks and processes them.
// If no logs are found, it fetches the header of the latest block in range to update progress.
func (f *Fetcher) fetchRangeWithLogs(ctx context.Context, start, end *big.Int) {
	startTime := time.Now()

	GetOrchestrator().DispatchLog("DEBUG", "🌀 Fetcher: Starting block range", "from", start.String(), "to", end.String())

	// Step 1: Range Filter
	filterQuery := ethereum.FilterQuery{
		FromBlock: start,
		ToBlock:   end,
	}

	if len(f.watchedAddresses) > 0 {
		filterQuery.Addresses = f.watchedAddresses
		// For specific addresses, we still filter by Transfer event to save RPC weight
		filterQuery.Topics = [][]common.Hash{{TransferEventHash}}
		Logger.Debug("🔍 Fetching logs with address filter",
			slog.String("from", start.String()),
			slog.String("to", end.String()),
			slog.Int("watched_count", len(f.watchedAddresses)))
	} else {
		// 🚀 Industrial Grade: Unfiltered mode captures EVERYTHING
		// No Topics = No Filter = All contract events captured
		filterQuery.Topics = nil
		Logger.Debug("🌍 Fetching logs for ALL events (Full Sniffing)",
			slog.String("from", start.String()),
			slog.String("to", end.String()))
	}

	var logs []types.Log
	var err error

	// 🚀 [Elegant Retry] for FilterLogs
	for retries := 0; retries < 5; retries++ {
		// 🛡️ 5600U 保护：增加硬超时，防止网络层挂起导致整个 Jobs 队列堵死
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		logs, err = f.pool.FilterLogs(reqCtx, filterQuery)
		cancel()

		if err == nil {
			GetOrchestrator().Dispatch(CmdFetchSuccess, nil)
			// 🚀 🔥 新增：通知扫描进度 (内存水位)
			GetOrchestrator().Dispatch(CmdNotifyFetched, end.Uint64())
			break
		}

		if isNotFound(err) {
			backoff := time.Duration(50*(1<<uint(retries))) * time.Millisecond
			slog.Debug("⏳ [Fetcher] FilterLogs not found, retrying...", "from", start, "to", end, "backoff", backoff)
			GetOrchestrator().Dispatch(CmdFetchFailed, "not_found")
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return
			}
		}
		break // Other errors handled by normal flow
	}

	if err != nil {
		// Log error and send results back
		select {
		case f.Results <- BlockData{Number: start, RangeEnd: end, Err: err}:
		case <-ctx.Done():
		case <-f.stopCh:
		}
		return
	}

	Logger.Debug("📊 RPC response received",
		slog.String("from", start.String()),
		slog.String("to", end.String()),
		slog.Int("logs_found", len(logs)),
		slog.Int("watched_addresses", len(f.watchedAddresses)))

	GetOrchestrator().DispatchLog("INFO", "📡 RPC response received",
		"range", fmt.Sprintf("%s-%s", start.String(), end.String()),
		"logs", len(logs))

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
			// 🚀 [Elegant Retry] for BlockByNumber
			for retries := 0; retries < 5; retries++ {
				// 🛡️ 5600U 保护：增加硬超时，防止网络层挂起导致消费端死锁
				reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				block, err = f.pool.BlockByNumber(reqCtx, bn)
				cancel()

				if err == nil {
					GetOrchestrator().Dispatch(CmdFetchSuccess, nil)
					break
				}

				if isNotFound(err) {
					backoff := time.Duration(50*(1<<uint(retries))) * time.Millisecond
					slog.Debug("⏳ [Fetcher] BlockByNumber not found, retrying...", "block", bn, "backoff", backoff)
					GetOrchestrator().Dispatch(CmdFetchFailed, "not_found")
					select {
					case <-time.After(backoff):
						continue
					case <-ctx.Done():
						return
					}
				}
				break
			}

			if err != nil {
				slog.Warn("⚠️ [FETCHER] Block fetch failed after retries", "block", bn, "err", err)
			}
		}

		if !f.sendResult(ctx, BlockData{
			Number:   bn,
			RangeEnd: end,
			Block:    block,
			Logs:     blockLogs,
			Err:      err,
		}) {
			return // ctx cancelled or stopped — abort remaining blocks in this job
		}

		// 🚀 🔥 新增：影子进度更新 (用于 UI 先行跳动)
		GetOrchestrator().Dispatch(CmdNotifyFetchProgress, bn.Uint64())
	}

	if f.metrics != nil {
		f.metrics.RecordFetcherJobCompleted(time.Since(startTime))
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "404")
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

// sendResult sends a BlockData to the Results channel.
// Returns true if the data was sent, false if ctx/stop fired.
// It NEVER drops data silently — dropping causes Sequencer gaps and deadlocks.
func (f *Fetcher) sendResult(ctx context.Context, data BlockData) bool {
	// 💾 录制原始数据：直接录制完整的 BlockData 对象，方便未来 100% 还原回放
	if f.recorder != nil && data.Err == nil {
		f.recorder.Record("block_data", data)
	}

	// 🚀 工业级节流：基于『交易笔数』进行硬限速
	if f.throughput != nil {
		tokens := len(data.Logs)
		if tokens == 0 {
			tokens = 1
		}
		if err := f.throughput.WaitN(ctx, tokens); err != nil {
			return false
		}
	}

	// 🚀 Pacemaker: 匀速发送，超时则跳过节拍器（不丢数据）
	if f.bpsLimiter != nil {
		limiterCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		err := f.bpsLimiter.Wait(limiterCtx)
		cancel()
		if err != nil {
			slog.Debug("⏳ [Fetcher] bpsLimiter wait timeout, proceeding cautiously")
		}
	}

	// 🔥 阻塞写入：等待 Sequencer 消费，不丢弃数据
	// 丢弃会导致 Sequencer expectedBlock 永远等不到该块，形成死锁
	select {
	case f.Results <- data:
		return true
	case <-ctx.Done():
		return false
	case <-f.stopCh:
		return false
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
