package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
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
	"web3-indexer-go/internal/database"
	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/recovery"
	"web3-indexer-go/internal/web"

	networkpkg "web3-indexer-go/pkg/network"

	"github.com/ethereum/go-ethereum/common"
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
	// 1. 获取链上实时高度
	latestChainBlock, rpcErr := rpcPool.GetLatestBlockNumber(ctx)

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
			if rpcErr != nil {
				return big.NewInt(0), nil
			}
			return new(big.Int).Add(latestChainBlock, big.NewInt(1)), nil
		}
		if blockNum, ok := new(big.Int).SetString(forceFrom, 10); ok {
			return blockNum, nil
		}
	}

	if cfg.StartBlockStr == "latest" {
		if rpcErr != nil {
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
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	resetDB := flag.Bool("reset", false, "Reset database")
	startFrom := flag.String("start-from", "", "Force start from: 'latest' or specific block number")
	mode := flag.String("mode", "index", "Operation mode: 'index' or 'replay'")
	replayFile := flag.String("file", "", "Trajectory file for replay (.jsonl or .lz4)")
	replaySpeed := flag.Float64("speed", 1.0, "Replay speed factor (e.g. 2.0 for 2x speed, 0 for max speed)")
	flag.Parse()
	cfg = config.Load()
	engine.InitLogger(cfg.LogLevel)
	forceFrom = *startFrom

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wsHub := web.NewHub()
	go wsHub.Run(ctx)

	// 🎬 处理回放模式
	if *mode == "replay" {
		if *replayFile == "" {
			slog.Error("❌ Replay mode requires -file parameter")
			return fmt.Errorf("replay mode requires -file parameter")
		}
		db, err := connectDB(ctx)
		if err != nil {
			return err
		}
		processor := engine.NewProcessor(db, nil, 100, cfg.ChainID, false, "replay")

		slog.Info("🏁 System starting in REPLAY mode.")
		if err := RunReplayMode(ctx, *replayFile, *replaySpeed, processor); err != nil {
			slog.Error("Replay failed", "err", err)
			return err
		}
		return nil
	}

	apiServer := NewServer(nil, wsHub, cfg.Port, cfg.AppTitle)
	recovery.WithRecovery(func() {
		slog.Info("🚀 Indexer API Server starting (Early Bird Mode)", "port", cfg.Port)
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("api_start_fail", "err", err)
		}
	}, "api_server")

	recovery.WithRecoveryNamed("async_init", func() {
		initEngine(ctx, apiServer, wsHub, *resetDB)
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	slog.Info("🏁 System Operational.")
	<-sigCh
	return nil
}

func initEngine(ctx context.Context, apiServer *Server, wsHub *web.Hub, resetDB bool) {
	slog.Info("⏳ Async engine initialization started...")

	// 🚨 Register Engine Health Watchdog: Broadcast panic to UI
	recovery.OnPanic = func(name string, err interface{}, _ string) {
		wsHub.Broadcast(web.WSEvent{
			Type: "engine_panic",
			Data: map[string]interface{}{
				"worker": name,
				"error":  fmt.Sprintf("%v", err),
				"ts":     time.Now().Unix(),
			},
		})
	}

	db, err := connectDB(ctx)
	if err != nil {
		return
	}

	// 🛡️ Ensure Schema is initialized (resolves token_metadata missing errors)
	if err := database.InitSchema(ctx, db); err != nil {
		slog.Error("❌ Database schema initialization failed", "err", err)
		return
	}

	rpcPool, err := setupRPC()
	if err != nil {
		return
	}

	if err := verifyNetworkWithRetry(); err != nil {
		return
	}

	// 🚀 Dynamic Speed Control: Enforce strict 1.0 TPS serial sync for Sepolia
	concurrency := cfg.FetchConcurrency
	tpsLimit := 1000.0 // Default for local Anvil
	if cfg.IsTestnet {
		concurrency = 1 // Force serial to prevent TPS bursts
		tpsLimit = 1.0  // 极度保守：每秒 1 笔交易，绝对保住额度
		slog.Info("🛡️ Quota protection ACTIVE: Enforcing 1.0 TPS strict serial sync")
	}

	sm := NewServiceManager(db, rpcPool, cfg.ChainID, cfg.RetryQueueSize, cfg.RPCRateLimit, cfg.RPCRateLimit*2, concurrency, cfg.EnableSimulator, cfg.NetworkMode, cfg.EnableRecording, cfg.RecordingPath)
	configureTokenFiltering(sm)
	sm.fetcher.SetThroughputLimit(tpsLimit)

	// 🚀 启动期基础设施对齐 (自愈)
	if err := sm.Processor.AlignInfrastructure(ctx, rpcPool); err != nil {
		slog.Error("❌ [FATAL] Infrastructure alignment failed", "err", err)
		// 严重错误，建议退出或降级，这里暂时记录错误
	}

	// 🚀 Industrial Guard: Align DB with Chain Absolute Truth
	guard := engine.NewConsistencyGuard(sm.Processor.GetRepoAdapter(), rpcPool)
	guard.SetDemoMode(cfg.DemoMode)

	// 🚀 Real-time reporting of alignment progress
	guard.OnStatus = func(status string, detail string, progress int) {
		wsHub.Broadcast(web.WSEvent{
			Type: "linearity_status",
			Data: map[string]interface{}{"status": status, "detail": detail, "progress": progress},
		})
	}

	if err := guard.PerformLinearityCheck(ctx); err != nil {
		slog.Error("linearity_check_failed", "err", err)
	}

	// ✨ On-Demand Lifecycle: Stay active for 5 mins after any heartbeat (Web access)
	lazyManager := engine.NewLazyManager(sm.fetcher, rpcPool, 5*time.Minute, guard)

	// 🚀 环境感知：如果是 Anvil 实验室环境，强制锁定为活跃状态，屏蔽休眠
	if cfg.ChainID == 31337 {
		lazyManager.SetAlwaysActive(true)
	}

	lazyManager.StartMonitor(ctx)

	// 🚀 Real-time status broadcasting
	lazyManager.OnStatus = func(status map[string]interface{}) {
		wsHub.Broadcast(web.WSEvent{Type: "lazy_status", Data: status})
	}

	apiServer.SetDependencies(db, rpcPool, lazyManager, sm.Processor, cfg.ChainID)

	// 🚀 Single Source of Truth for Activity: All WS connections wake up the engine
	wsHub.OnActivity = func() {
		lazyManager.Trigger()
	}

	// 🎨 On-Demand Coloring: When UI scrolls to a token, fetch its metadata
	wsHub.OnNeedMeta = func(addr string) {
		slog.Debug("🎨 [On-Demand] UI requested metadata", "address", addr)
		_ = sm.Processor.GetSymbol(common.HexToAddress(addr))
	}

	sm.Processor.EventHook = func(eventType string, data interface{}) {
		if apiServer.signer != nil {
			if signed, err := apiServer.signer.Sign(eventType, data); err == nil {
				wsHub.Broadcast(signed)
				return
			}
		}
		wsHub.Broadcast(web.WSEvent{Type: eventType, Data: data})
	}

	startBlock, err := sm.GetStartBlock(ctx, forceFrom, resetDB)
	if err != nil {
		slog.Error("failed_to_get_start_block", "err", err)
		return
	}

	setupParentAnchor(ctx, db, rpcPool, startBlock)
	initServices(ctx, sm, startBlock)
}

func connectDB(ctx context.Context) (*sqlx.DB, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := sqlx.ConnectContext(dbCtx, "pgx", cfg.DatabaseURL)
	if err != nil {
		slog.Error("❌ Database connection failed", "err", err)
	}
	return db, err
}

func setupRPC() (engine.RPCClient, error) {
	var rpcPool engine.RPCClient
	var err error
	if cfg.IsTestnet {
		rpcPool, err = engine.NewEnhancedRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.IsTestnet, cfg.MaxSyncBatch, cfg.RPCTimeout)
	} else {
		rpcPool, err = engine.NewRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.RPCTimeout)
	}
	if err == nil {
		rpcPool.SetRateLimit(float64(cfg.RPCRateLimit), cfg.RPCRateLimit*2)
	}
	return rpcPool, err
}

func verifyNetworkWithRetry() error {
	var verifyErr error
	for i := 0; i < 3; i++ {
		if ethClient, err := ethclient.Dial(cfg.RPCURLs[0]); err == nil {
			verifyErr = networkpkg.VerifyNetwork(ethClient, cfg.ChainID)
			ethClient.Close()
			if verifyErr == nil {
				return nil
			}
		} else {
			verifyErr = err
		}
		time.Sleep(2 * time.Second)
	}
	slog.Error("❌ [FATAL] Network verification failed", "error", verifyErr)
	return verifyErr
}

func configureTokenFiltering(sm *ServiceManager) {
	var watched []string
	switch cfg.TokenFilterMode {
	case "all":
		watched = []string{}
	case "whitelist":
		watched = cfg.WatchedTokenAddresses
	default:
		watched = []string{}
	}
	sm.fetcher.SetWatchedAddresses(watched)
	sm.Processor.SetWatchedAddresses(watched)
}

func setupParentAnchor(ctx context.Context, db *sqlx.DB, rpcPool engine.RPCClient, startBlock *big.Int) {
	if startBlock.Cmp(big.NewInt(0)) <= 0 {
		return
	}
	parentNum := new(big.Int).Sub(startBlock, big.NewInt(1))
	anchorCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if parent, err := rpcPool.BlockByNumber(anchorCtx, parentNum); err == nil && parent != nil {
		if _, err := db.Exec("INSERT INTO blocks (number, hash, parent_hash, timestamp) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
			parentNum.String(), parent.Hash().Hex(), parent.ParentHash().Hex(), parent.Time()); err != nil {
			slog.Warn("failed_to_insert_parent_block_anchor", "err", err)
		}
	}
}

func initServices(ctx context.Context, sm *ServiceManager, startBlock *big.Int) {
	var wg sync.WaitGroup
	sm.fetcher.Start(ctx, &wg)
	sequencer := engine.NewSequencerWithFetcher(sm.Processor, sm.fetcher, startBlock, cfg.ChainID, sm.fetcher.Results, make(chan error, 1), nil, engine.GetMetrics())

	wg.Add(1)
	go runSequencerWithSelfHealing(ctx, sequencer, &wg)
	go recovery.WithRecoveryNamed("tail_follow", func() { sm.StartTailFollow(ctx, startBlock) })

	if cfg.EnableSimulator {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 🚀 CHAOS_MODE: Boosted to 10 TPS for high-fidelity simulation
			proSim := engine.NewProSimulator(cfg.RPCURLs[0], true, 10)
			proSim.Start()
		}()
	}
}

func continuousTailFollow(ctx context.Context, fetcher *engine.Fetcher, rpcPool engine.RPCClient, startBlock *big.Int) {
	slog.Info("🐕 [TailFollow] Starting continuous tail follow", "start_block", startBlock.String())
	lastScheduled := new(big.Int).Sub(startBlock, big.NewInt(1))
	// 🚀 Industrial Grade optimization: Ultra-fast polling for local Anvil labs (500ms)
	ticker := time.NewTicker(500 * time.Millisecond)
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
				if tickCount%10 == 1 {
					slog.Warn("🐕 [TailFollow] Failed to get tip", "err", err)
				}
			} else if tip.Cmp(lastScheduled) > 0 {
				// 🚀 Update chain height metric for UI sync
				engine.GetMetrics().UpdateChainHeight(tip.Int64())

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
