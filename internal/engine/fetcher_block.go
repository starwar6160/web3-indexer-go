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
func (f *Fetcher) fetchRangeWithLogs(ctx context.Context, start, end *big.Int) {
	startTime := time.Now()
	GetOrchestrator().DispatchLog("DEBUG", "🌀 Fetcher: Starting block range", "from", start.String(), "to", end.String())

	// Step 1: Execute FilterLogs with retry
	logs, err := f.executeFilterLogsWithRetry(ctx, start, end)
	if err != nil {
		f.reportRangeError(ctx, start, end, err)
		return
	}

	// Step 2: Group logs and process the range
	logsByBlock := f.groupLogsByBlock(logs)
	f.processBlockRange(ctx, start, end, logsByBlock)

	if f.metrics != nil {
		f.metrics.RecordFetcherJobCompleted(time.Since(startTime))
	}
}

func (f *Fetcher) buildFilterQuery(start, end *big.Int) ethereum.FilterQuery {
	q := ethereum.FilterQuery{FromBlock: start, ToBlock: end}
	if len(f.watchedAddresses) > 0 {
		q.Addresses = f.watchedAddresses
		q.Topics = [][]common.Hash{{TransferEventHash}}
	}
	return q
}

func (f *Fetcher) executeFilterLogsWithRetry(ctx context.Context, start, end *big.Int) ([]types.Log, error) {
	query := f.buildFilterQuery(start, end)
	var logs []types.Log
	var err error

	for retries := 0; retries < 5; retries++ {
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		logs, err = f.pool.FilterLogs(reqCtx, query)
		cancel()

		if err == nil {
			GetOrchestrator().Dispatch(CmdFetchSuccess, nil)
			GetOrchestrator().Dispatch(CmdNotifyFetched, end.Uint64())
			return logs, nil
		}

		if isNotFound(err) {
			backoff := time.Duration(50*(1<<uint(retries))) * time.Millisecond
			GetOrchestrator().Dispatch(CmdFetchFailed, "not_found")
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		break
	}
	return nil, err
}

func (f *Fetcher) groupLogsByBlock(logs []types.Log) map[uint64][]types.Log {
	m := make(map[uint64][]types.Log)
	for _, l := range logs {
		m[l.BlockNumber] = append(m[l.BlockNumber], l)
	}
	return m
}

func (f *Fetcher) processBlockRange(ctx context.Context, start, end *big.Int, logsByBlock map[uint64][]types.Log) {
	for i := new(big.Int).Set(start); i.Cmp(end) <= 0; i.Add(i, big.NewInt(1)) {
		bn := new(big.Int).Set(i)
		blockLogs := logsByBlock[bn.Uint64()]

		var block *types.Block
		var err error

		if len(blockLogs) > 0 || bn.Cmp(end) == 0 {
			block, err = f.fetchBlockWithRetry(ctx, bn)
		}

		if !f.sendResult(ctx, BlockData{
			Number:   bn,
			RangeEnd: end,
			Block:    block,
			Logs:     blockLogs,
			Err:      err,
		}) {
			return
		}
		GetOrchestrator().Dispatch(CmdNotifyFetchProgress, bn.Uint64())
	}
}

func (f *Fetcher) fetchBlockWithRetry(ctx context.Context, bn *big.Int) (*types.Block, error) {
	var block *types.Block
	var err error
	for retries := 0; retries < 5; retries++ {
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		block, err = f.pool.BlockByNumber(reqCtx, bn)
		cancel()

		if err == nil {
			GetOrchestrator().Dispatch(CmdFetchSuccess, nil)
			return block, nil
		}

		if isNotFound(err) {
			backoff := time.Duration(50*(1<<uint(retries))) * time.Millisecond
			GetOrchestrator().Dispatch(CmdFetchFailed, "not_found")
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		break
	}
	return nil, err
}

func (f *Fetcher) reportRangeError(ctx context.Context, start, end *big.Int, err error) {
	select {
	case f.Results <- BlockData{Number: start, RangeEnd: end, Err: err}:
	case <-ctx.Done():
	case <-f.stopCh:
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
