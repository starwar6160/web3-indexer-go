package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/recovery"
	"web3-indexer-go/internal/web"

	"github.com/jmoiron/sqlx"
)

func initServices(ctx context.Context, sm *ServiceManager, startBlock *big.Int, lazyManager *engine.LazyManager, rpcPool engine.RPCClient, wsHub *web.Hub) {
	if cfg.ChainID == 31337 {
		AlignAnvilData(ctx, sm.db, rpcPool)
	}

	var wg sync.WaitGroup
	sm.fetcher.Start(ctx, &wg)
	sequencer := engine.NewSequencerWithFetcher(sm.Processor, sm.fetcher, startBlock, cfg.ChainID, sm.fetcher.Results, make(chan error, 100), nil, engine.GetMetrics())
	sm.fetcher.SetSequencer(sequencer)

	strategy := engine.GetStrategy(cfg.ChainID)
	orchestrator := engine.GetOrchestrator()
	orchestrator.Init(ctx, sm.fetcher, strategy)
	if err := strategy.OnStartup(ctx, orchestrator, sm.db, cfg.ChainID); err != nil {
		slog.Error("❌ Strategy startup failed", "err", err)
	}

	asyncWriter := engine.NewAsyncWriter(sm.Processor.GetDB(), orchestrator, !strategy.ShouldPersist(), cfg.ChainID)
	orchestrator.SetAsyncWriter(asyncWriter)
	asyncWriter.Start()

	healer := engine.NewSelfHealer(orchestrator)
	go healer.Start(ctx)

	watchdog := engine.NewDeadlockWatchdog(cfg.ChainID, cfg.DemoMode, sequencer, sm.Processor.GetRepoAdapter(), rpcPool, lazyManager, engine.GetMetrics())
	watchdog.SetFetcher(sm.fetcher)
	watchdog.Enable()
	watchdog.OnHealingTriggered = func(event engine.HealingEvent) {
		wsHub.Broadcast(web.WSEvent{Type: "system_healing", Data: event})
	}
	watchdog.Start(ctx)

	wg.Add(1)
	go runSequencerWithSelfHealing(ctx, sequencer, &wg)
	go recovery.WithRecoveryNamed("tail_follow", func() { sm.StartTailFollow(ctx, startBlock) })

	if cfg.EnableSimulator {
		wg.Add(1)
		go func() {
			defer wg.Done()
			engine.NewProSimulator(cfg.RPCURLs[0], true, 10).Start()
		}()
	}
}

func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, rpcPool engine.RPCClient, chainID int64, forceFrom string, resetDB bool) (*big.Int, error) {
	latestChainBlock, rpcErr := rpcPool.GetLatestBlockNumber(ctx)
	if resetDB {
		if _, err := db.ExecContext(ctx, "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;"); err != nil {
			return nil, fmt.Errorf("reset database failed: %w", err)
		}
		return getDefaultStartBlockForChain(chainID), nil
	}
	if forceFrom != "" {
		if forceFrom == "latest" {
			if rpcErr != nil {
				return nil, fmt.Errorf("get latest block for forceFrom=latest: %w", rpcErr)
			}
			return new(big.Int).Add(latestChainBlock, big.NewInt(1)), nil
		}
		if blockNum, ok := new(big.Int).SetString(forceFrom, 10); ok {
			return blockNum, nil
		}
		return nil, fmt.Errorf("invalid forceFrom block number: %q", forceFrom)
	}
	if cfg.StartBlockStr == "latest" {
		if rpcErr != nil {
			return nil, fmt.Errorf("get latest block for StartBlockStr=latest: %w", rpcErr)
		}
		startBlock := new(big.Int).Sub(latestChainBlock, big.NewInt(6))
		if startBlock.Cmp(big.NewInt(0)) < 0 {
			startBlock = big.NewInt(0)
		}
		return startBlock, nil
	}
	if cfg.StartBlock > 0 {
		return new(big.Int).SetInt64(cfg.StartBlock), nil
	}
	var lastSyncedBlock string
	if err := db.GetContext(ctx, &lastSyncedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID); err != nil {
		slog.Debug("📊 No checkpoint found, starting from scratch", "chain_id", chainID, "err", err)
	}
	if lastSyncedBlock == "" {
		return getDefaultStartBlockForChain(chainID), nil
	}
	blockNum, ok := new(big.Int).SetString(lastSyncedBlock, 10)
	if !ok {
		return nil, fmt.Errorf("invalid checkpoint block number: %q", lastSyncedBlock)
	}
	return new(big.Int).Add(blockNum, big.NewInt(1)), nil
}

func getDefaultStartBlockForChain(chainID int64) *big.Int {
	switch chainID {
	case 11155111:
		return big.NewInt(10262444)
	default:
		return big.NewInt(0)
	}
}

func runSequencerWithSelfHealing(ctx context.Context, sequencer *engine.Sequencer, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			recovery.WithRecoveryNamed("sequencer_run", func() { sequencer.Run(ctx) })
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func setupParentAnchor(ctx context.Context, db *sqlx.DB, rpcPool engine.RPCClient, startBlock *big.Int) {
	if startBlock.Cmp(big.NewInt(0)) <= 0 {
		return
	}
	parentNum := new(big.Int).Sub(startBlock, big.NewInt(1))
	if parent, err := rpcPool.BlockByNumber(ctx, parentNum); err == nil && parent != nil {
		if _, err := db.Exec("INSERT INTO blocks (number, hash, parent_hash, timestamp) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
			parentNum.String(), parent.Hash().Hex(), parent.ParentHash().Hex(), parent.Time()); err != nil {
			slog.Warn("❌ Failed to insert parent anchor", "err", err, "block", parentNum.String())
		}
	}
}

func continuousTailFollow(ctx context.Context, fetcher *engine.Fetcher, rpcPool engine.RPCClient, startBlock *big.Int) {
	lastScheduled := new(big.Int).Sub(startBlock, big.NewInt(1))
	tickerInterval := 500 * time.Millisecond
	schedulingWindow := big.NewInt(10)
	if cfg.ChainID == 31337 {
		tickerInterval = 100 * time.Millisecond
		schedulingWindow = big.NewInt(100)
	}
	ticker := time.NewTicker(tickerInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if tip, err := rpcPool.GetLatestBlockNumber(ctx); err == nil {
				orch := engine.GetOrchestrator()
				orch.UpdateChainHead(tip.Uint64())
				snap := orch.GetSnapshot()
				targetHeight := big.NewInt(int64(snap.TargetHeight))

				if targetHeight.Cmp(lastScheduled) > 0 {
					nextBlock := new(big.Int).Add(lastScheduled, big.NewInt(1))
					aggressiveTarget := new(big.Int).Add(targetHeight, schedulingWindow)
					if aggressiveTarget.Cmp(tip) > 0 {
						aggressiveTarget = new(big.Int).Set(tip)
					}
					if nextBlock.Cmp(aggressiveTarget) <= 0 {
						// 🔴 Critical Fix: 仅在调度成功时推进 lastScheduled
						// 防止 Schedule 失败时跳过范围，造成数据缺口
						if err := fetcher.Schedule(ctx, nextBlock, aggressiveTarget); err == nil {
							lastScheduled.Set(aggressiveTarget)
						} else {
							slog.Warn("⚠️ [TailFollow] Schedule failed, keeping cursor",
								"nextBlock", nextBlock,
								"target", aggressiveTarget,
								"err", err)
						}
					}
				}
			}
		}
	}
}

// AlignAnvilData 强制对齐 Anvil 数据：如果 DB 高度 > RPC 高度，则削峰
func AlignAnvilData(ctx context.Context, db *sqlx.DB, rpcPool engine.RPCClient) {
	tip, err := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		slog.Error("⚠️ [AlignAnvil] Failed to get RPC tip", "err", err)
		return
	}
	rpcHeight := tip.Uint64()

	var dbHeight uint64
	err = db.GetContext(ctx, &dbHeight, "SELECT COALESCE(MAX(number), 0) FROM blocks")
	if err != nil {
		slog.Error("⚠️ [AlignAnvil] Failed to get DB height", "err", err)
		return
	}

	if dbHeight > rpcHeight {
		slog.Warn("🚨 [AlignAnvil] DATA INVERSION DETECTED",
			"db_height", dbHeight,
			"rpc_height", rpcHeight,
			"gap", dbHeight-rpcHeight)

		// 削峰填谷：删除所有高于 RPC 的数据
		slog.Warn("🔪 [AlignAnvil] Executing CUTOFF...")
		_, err = db.ExecContext(ctx, "DELETE FROM blocks WHERE number > $1", rpcHeight)
		if err != nil {
			slog.Error("❌ [AlignAnvil] Cutoff failed", "err", err)
			return
		}
		// Transfers 会级联删除 (ON DELETE CASCADE)

		// 重置 Checkpoint
		_, err = db.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1 WHERE chain_id = 31337", rpcHeight)
		if err != nil {
			slog.Error("❌ [AlignAnvil] Checkpoint reset failed", "err", err)
			return
		}

		slog.Info("✅ [AlignAnvil] Data successfully aligned to RPC height", "new_height", rpcHeight)
	} else {
		slog.Info("✅ [AlignAnvil] Data integrity check passed", "db", dbHeight, "rpc", rpcHeight)
	}
}
