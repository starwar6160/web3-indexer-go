package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/emulator"
	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/recovery"
	"web3-indexer-go/internal/web"

	networkpkg "web3-indexer-go/pkg/network"

	"github.com/ethereum/go-ethereum/ethclient"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// DBWrapper wraps sqlx.DB to match the DBInterface
type DBWrapper struct {
	db *sqlx.DB
}

func (w *DBWrapper) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return w.db.ExecContext(ctx, query, args...)
}

var (
	cfg               *config.Config
	selfHealingEvents atomic.Uint64
	forceFrom         string
	Version           = "v2.2.0-intelligence-engine" // 🚀 工业级版本号
)

func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, rpcPool engine.RPCClient, chainID int64, forceFrom string, resetDB bool) (*big.Int, error) {
	if resetDB {
		slog.Info("🚨 DATABASE_RESET_REQUESTED", "action", "wiping_tables")
		_, err := db.ExecContext(ctx, "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;")
		if err != nil {
			slog.Error("reset_db_fail", "err", err)
		}
		return big.NewInt(10262444), nil
	}

	if forceFrom != "" {
		if forceFrom == "latest" {
			latestChainBlock, err := rpcPool.GetLatestBlockNumber(ctx)
			if err != nil {
				return big.NewInt(0), nil
			}
			return new(big.Int).Add(latestChainBlock, big.NewInt(1)), nil
		}
		if blockNum, ok := new(big.Int).SetString(forceFrom, 10); ok {
			return blockNum, nil
		}
	}

	if cfg.StartBlockStr == "latest" {
		latestChainBlock, err := rpcPool.GetLatestBlockNumber(ctx)
		if err != nil {
			return big.NewInt(0), nil
		}
		reorgSafetyOffset := int64(6)
		startBlock := new(big.Int).Sub(latestChainBlock, big.NewInt(reorgSafetyOffset))
		if startBlock.Cmp(big.NewInt(0)) < 0 {
			startBlock = big.NewInt(0)
		}
		return startBlock, nil
	}

	if cfg.StartBlock > 0 {
		return new(big.Int).SetInt64(cfg.StartBlock), nil
	}

	var lastSyncedBlock string
	err := db.GetContext(ctx, &lastSyncedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
	if err != nil || lastSyncedBlock == "" {
		return big.NewInt(10262444), nil
	}

	blockNum, _ := new(big.Int).SetString(lastSyncedBlock, 10)
	if cfg.IsTestnet && chainID == 11155111 {
		minStartBlock := big.NewInt(10262444)
		if blockNum.Cmp(minStartBlock) < 0 {
			return minStartBlock, nil
		}
	}

	return new(big.Int).Add(blockNum, big.NewInt(1)), nil
}

// runSequencerWithSelfHealing 启动 Sequencer 并在崩溃后自动重启
func runSequencerWithSelfHealing(ctx context.Context, sequencer *engine.Sequencer, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			slog.Info("🛑 [SELF-HEAL] Sequencer supervisor stopped")
			return
		default:
			slog.Info("🔄 [SELF-HEAL] Starting Sequencer...")
			recovery.WithRecoveryNamed("sequencer_run", func() {
				sequencer.Run(ctx)
			})

			// 如果 Sequencer 崩溃退出，等待 3 秒后重启
			slog.Warn("⚠️ [SELF-HEAL] Sequencer crashed, restarting in 3s...")
			select {
			case <-ctx.Done():
				slog.Info("🛑 [SELF-HEAL] Sequencer supervisor cancelled during restart delay")
				return
			case <-time.After(3 * time.Second):
				slog.Info("♻️ [SELF-HEAL] Sequencer restarting...")
			}
		}
	}
}

func main() {
	resetDB := flag.Bool("reset", false, "Reset database")
	startFrom := flag.String("start-from", "", "Force start from: 'latest' or specific block number")
	flag.Parse()
	cfg = config.Load()
	engine.InitLogger(cfg.LogLevel)
	forceFrom = *startFrom

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 🚀 SRE 核心：先开 Web 服务器，再连数据库和 RPC
	wsHub := web.NewHub()
	go wsHub.Run(ctx)

	apiPort := cfg.Port
	if os.Getenv("PORT") != "" {
		apiPort = os.Getenv("PORT")
	}

	apiServer := NewServer(nil, wsHub, apiPort, cfg.AppTitle)
	
	// 🚀 关键修复：异步启动 API Server，不再阻塞下方 Engine 初始化
	recovery.WithRecovery(func() {
		slog.Info("🚀 Indexer API Server starting (Early Bird Mode)", "port", apiPort)
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("api_start_fail", "err", err)
		}
	}, "api_server")

	// --- 接下来执行初始化 (不再被阻塞) ---
	recovery.WithRecoveryNamed("async_init", func() {
		slog.Info("⏳ Async engine initialization started...")
		
		// 1. 数据库连接 (增加超时保护)
		dbCtx, dbCancel := context.WithTimeout(ctx, 15*time.Second)
		db, err := sqlx.ConnectContext(dbCtx, "pgx", cfg.DatabaseURL)
		dbCancel()
		
		if err != nil {
			slog.Error("❌ Database connection failed (Fatal for Engine)", "err", err, "url", "hidden")
			return
		}
		slog.Info("✅ Database connected successfully")

		var rpcPool engine.RPCClient
		if cfg.IsTestnet {
			rpcPool, err = engine.NewEnhancedRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.IsTestnet, cfg.MaxSyncBatch, cfg.RPCTimeout)
		} else {
			rpcPool, err = engine.NewRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.RPCTimeout)
		}
		if err != nil {
			slog.Error("rpc_init_fail", "err", err)
			return
		}
		rpcPool.SetRateLimit(float64(cfg.RPCRateLimit), cfg.RPCRateLimit*2)

		// ✅ 工业级启动预检：强制校验 Network ID
		slog.Info("🛡️ Performing startup network verification...")
		var verifyErr error
		for i := 0; i < 3; i++ {
			ethClient, err := ethclient.Dial(cfg.RPCURLs[0])
			if err == nil {
				verifyErr = networkpkg.VerifyNetwork(ethClient, cfg.ChainID)
				ethClient.Close()
				if verifyErr == nil {
					break
				}
			} else {
				verifyErr = err
			}
			slog.Warn("🛡️ Network verification failed, retrying...", "attempt", i+1, "error", verifyErr)
			time.Sleep(2 * time.Second)
		}

		if verifyErr != nil {
			slog.Error("❌ [FATAL] Startup network verification failed permanently", "error", verifyErr)
			return
		}

		sm := NewServiceManager(db, rpcPool, cfg.ChainID, cfg.RetryQueueSize, cfg.RPCRateLimit, cfg.RPCRateLimit*2, cfg.FetchConcurrency, cfg.EnableSimulator, cfg.NetworkMode)

		// ✨ Configure token filtering based on TOKEN_FILTER_MODE
		if cfg.TokenFilterMode == "all" {
			// Query all Transfer events from all contracts
			slog.Info("🌍 TOKEN_FILTER_MODE=all", "action", "monitoring_all_transfers", "watched_count", 0)
			// Explicitly set empty addresses to query all
			sm.fetcher.SetWatchedAddresses([]string{})
			sm.processor.SetWatchedAddresses([]string{})
		} else if cfg.TokenFilterMode == "whitelist" {
			if len(cfg.WatchedTokenAddresses) > 0 {
				slog.Info("🎯 TOKEN_FILTER_MODE=whitelist", "action", "monitoring_specific_tokens", "watched_count", len(cfg.WatchedTokenAddresses))
				sm.fetcher.SetWatchedAddresses(cfg.WatchedTokenAddresses)
				sm.processor.SetWatchedAddresses(cfg.WatchedTokenAddresses)
			} else {
				slog.Warn("⚠️ TOKEN_FILTER_MODE=whitelist but no addresses provided, falling back to 'all' mode")
				sm.fetcher.SetWatchedAddresses([]string{})
				sm.processor.SetWatchedAddresses([]string{})
			}
		} else {
			slog.Error("❌ Invalid TOKEN_FILTER_MODE", "mode", cfg.TokenFilterMode, "falling_back_to", "all")
			sm.fetcher.SetWatchedAddresses([]string{})
			sm.processor.SetWatchedAddresses([]string{})
		}

		// ✨ 注入依赖，使 API 变得可用
		lazyManager := engine.NewLazyManager(sm.fetcher, rpcPool, 3*time.Minute, 3*time.Minute)
		apiServer.SetDependencies(db, rpcPool, lazyManager, cfg.ChainID)

		slog.Info("🚀 System initialized", 
			"chain_id", cfg.ChainID, 
			"rpc_url", cfg.RPCURLs[0],
			"db_url", "connected",
			"demo_mode", cfg.DemoMode,
		)

		sm.processor.EventHook = func(eventType string, data interface{}) {
			// 🛡️ WebSocket 实时流签名
			if apiServer.signer != nil {
				signed, err := apiServer.signer.Sign(eventType, data)
				if err == nil {
					wsHub.Broadcast(signed)
				} else {
					wsHub.Broadcast(web.WSEvent{Type: eventType, Data: data})
				}
			} else {
				wsHub.Broadcast(web.WSEvent{Type: eventType, Data: data})
			}
		}

		startBlock, err := sm.GetStartBlock(ctx, forceFrom, *resetDB)
		if err != nil {
			slog.Error("failed_to_get_start_block", "err", err)
			return
		}

		// 补全父块锚点 (优化：增加 10s 超时保护)
		if startBlock.Cmp(big.NewInt(0)) > 0 {
			parentBlockNum := new(big.Int).Sub(startBlock, big.NewInt(1))
			anchorCtx, anchorCancel := context.WithTimeout(ctx, 10*time.Second)
			parentBlock, err := rpcPool.BlockByNumber(anchorCtx, parentBlockNum)
			anchorCancel()
			
			if err == nil && parentBlock != nil {
				if _, err := db.Exec("INSERT INTO blocks (number, hash, parent_hash, timestamp) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
					parentBlockNum.String(), parentBlock.Hash().Hex(), parentBlock.ParentHash().Hex(), parentBlock.Time()); err != nil {
					slog.Warn("failed_to_insert_parent_block", "err", err)
				}
			} else {
				slog.Warn("⚠️ Could not fetch parent block anchor, proceeding anyway", "err", err)
			}
		}

		var wg sync.WaitGroup
		sm.fetcher.Start(ctx, &wg)
		fatalErrCh := make(chan error, 1)
		sequencer := engine.NewSequencerWithFetcher(sm.processor, sm.fetcher, startBlock, cfg.ChainID, sm.fetcher.Results, fatalErrCh, nil, engine.GetMetrics())

		wg.Add(1)
		slog.Info("⛓️ Engine Components Ignited", "start_block", startBlock.String())

		// 🚀 自愈 Sequencer：崩溃后自动重启
		go runSequencerWithSelfHealing(ctx, sequencer, &wg)

		go recovery.WithRecoveryNamed("tail_follow", func() { sm.StartTailFollow(ctx, startBlock) })

		// 🏭 Pro Simulator：工业级持续交易模拟器
		if cfg.EnableSimulator {
			proSim := engine.NewProSimulator(cfg.RPCURLs[0], true, 2) // 2 TPS
			wg.Add(1)
			go recovery.WithRecoveryNamed("pro_simulator", func() {
				defer wg.Done()
				slog.Info("🏭 [PRO_SIM] Starting industrial simulator", "tps", 2, "rpc_url", cfg.RPCURLs[0])
				proSim.Start()
			})
		}

		// 仿真器 (仅 demo，已废弃，保留 Pro Simulator)
		if cfg.EnableSimulator && cfg.DemoMode {
			emuCfg := emulator.LoadConfig()
			if emuCfg.Enabled {
				emu, err := emulator.NewEmulator(cfg.RPCURLs[0], emuCfg.PrivateKey)
				if err == nil && emu != nil {
					wg.Add(1)
					recovery.WithRecoveryNamed("emulator_start", func() { defer wg.Done(); _ = emu.Start(ctx, nil) })
				}
			}
		}
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	slog.Info("🏁 System Operational.")
	<-sigCh
	cancel()
}

func continuousTailFollow(ctx context.Context, fetcher *engine.Fetcher, rpcPool engine.RPCClient, startBlock *big.Int) {
	slog.Info("🐕 [TailFollow] Starting continuous tail follow", "start_block", startBlock.String())
	lastScheduled := new(big.Int).Sub(startBlock, big.NewInt(1))
	ticker := time.NewTicker(2 * time.Second)
	tickCount := 0
	for {
		select {
		case <-ctx.Done():
			slog.Info("🐕 [TailFollow] Context cancelled, stopping")
			return
		case <-ticker.C:
			tickCount++
			tip, err := rpcPool.GetLatestBlockNumber(ctx)
			if err != nil {
				if tickCount%10 == 1 { // 每 20 秒打印一次错误
					slog.Warn("🐕 [TailFollow] Failed to get tip", "err", err)
				}
			} else if tip.Cmp(lastScheduled) > 0 {
				nextBlock := new(big.Int).Add(lastScheduled, big.NewInt(1))
				slog.Info("🐕 [TailFollow] Scheduling new range", "from", nextBlock.String(), "to", tip.String())
				if err := fetcher.Schedule(ctx, nextBlock, tip); err != nil {
					slog.Error("🐕 [TailFollow] Failed to schedule", "err", err)
				}
				lastScheduled.Set(tip)
			}
		}
	}
}
