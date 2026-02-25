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
		// ChainID-aware default start block
		return getDefaultStartBlockForChain(chainID), nil
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
		// ChainID-aware default start block
		return getDefaultStartBlockForChain(chainID), nil
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

// getDefaultStartBlockForChain 返回基于 ChainID 的默认起始块
func getDefaultStartBlockForChain(chainID int64) *big.Int {
	switch chainID {
	case 31337: // Anvil
		return big.NewInt(0)
	case 11155111: // Sepolia
		return big.NewInt(10262444)
	case 1: // Mainnet
		return big.NewInt(0)
	default:
		return big.NewInt(0)
	}
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

	// 🔥 横滨实验室优化：使用带节流的 WebSocket Hub
	// 避免高频 Anvil 环境下的 WS 连接震荡
	var wsHub *web.Hub
	if cfg.ChainID == 31337 {
		// Anvil 环境：500ms 节流间隔，聚合高频事件
		throttledHub := web.NewThrottledHub(500 * time.Millisecond)
		go throttledHub.RunWithThrottling(ctx)
		wsHub = throttledHub.Hub // 提取基础 Hub 供其他组件使用
		slog.Info("🔥 Throttled WebSocket Hub activated for Anvil", "throttle", "500ms")
	} else {
		// 生产环境：使用标准 Hub
		wsHub = web.NewHub()
		go wsHub.Run(ctx)
		slog.Info("📡 Standard WebSocket Hub activated for production")
	}

	// 🎬 处理回放模式
	if *mode == "replay" {
		if *replayFile == "" {
			slog.Error("❌ Replay mode requires -file parameter")
			return fmt.Errorf("replay mode requires -file parameter")
		}
		db, err := connectDB(ctx, cfg.ChainID == 31337)
		if err != nil {
			return err
		}
		processor := engine.NewProcessor(db, nil, 100, cfg.ChainID, false, "replay")

		// 🔥 横滨实验室：回放模式也启用异步落盘
		orchestrator := engine.GetOrchestrator()
		asyncWriter := engine.NewAsyncWriter(db, orchestrator, cfg.EphemeralMode, cfg.ChainID)
		orchestrator.SetAsyncWriter(asyncWriter)
		asyncWriter.Start()

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

	db, err := connectDB(ctx, cfg.ChainID == 31337)
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

	// 🚀 Dynamic Speed Control: 环境感知性能配置
	// 获取性能配置文件（自动检测 Anvil/生产环境）
	perfProfile := engine.GetPerformanceProfile(cfg.RPCURLs, cfg.ChainID)
	perfProfile.ApplyToConfig(cfg)

	// 应用性能配置
	concurrency := cfg.FetchConcurrency
	tpsLimit := cfg.RPCRateLimit

	if perfProfile.EnableAggressiveBatch {
		// 🔥 横滨实验室极限性能配置
		concurrency = perfProfile.FetchConcurrency
		tpsLimit = int(perfProfile.TPSLimit)
		slog.Info("🔥 YOKOHAMA LAB PROFILE ACTIVATED",
			"concurrency", concurrency,
			"tps_limit", tpsLimit,
			"batch_size", perfProfile.BatchSize,
			"channel_buffer", perfProfile.ChannelBufferSize,
		)
	} else if cfg.IsTestnet {
		// 🛡️ Sepolia 生产环境：极度保守
		concurrency = 1
		tpsLimit = 1
		slog.Info("🛡️ Quota protection ACTIVE: Enforcing 1.0 TPS strict serial sync")
	}

	sm := NewServiceManager(db, rpcPool, cfg.ChainID, cfg.RetryQueueSize, tpsLimit, tpsLimit*2, concurrency, cfg.EnableSimulator, cfg.NetworkMode, cfg.EnableRecording, cfg.RecordingPath)
	configureTokenFiltering(sm)
	sm.fetcher.SetThroughputLimit(float64(tpsLimit))

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

	// 📐 Configure HeightOracle with drift policy from config.
	engine.GetHeightOracle().StrictHeightCheck = true
	engine.GetHeightOracle().DriftTolerance = 5
	if cfg != nil {
		engine.GetHeightOracle().StrictHeightCheck = cfg.StrictHeightCheck
		engine.GetHeightOracle().DriftTolerance = cfg.DriftTolerance
	}

	// ✨ On-Demand Lifecycle: Stay active for 5 mins after any heartbeat (Web access)
	lazyManager := engine.NewLazyManager(sm.fetcher, rpcPool, 5*time.Minute, guard)

	// 🔥 设置 ServiceManager 的 lazyManager 引用（用于区块链活动通知）
	sm.lazyManager = lazyManager

	// 🔥 横滨实验室环境强制旁路：彻底禁用 Eco-Mode 和配额限制
	if cfg.ChainID == 31337 || engine.IsLocalAnvil(cfg.RPCURLs[0]) {
		lazyManager.SetAlwaysActive(true)
		slog.Info("🔥 YOKOHAMA LAB BYPASS: Eco-Mode and Quota Enforcement disabled indefinitely",
			"chain_id", cfg.ChainID,
			"rpc", cfg.RPCURLs[0])

		// 🔥 设置无限配额
		sm.fetcher.SetThroughputLimit(100000.0)
		slog.Info("🔥 YOKOHAMA LAB BYPASS: Quota set to unlimited (100k TPS)")
	}

	// 🔥 更新 Prometheus 指标
	labModeEnabled := cfg.ChainID == 31337 || cfg.ForceAlwaysActive
	engine.GetMetrics().SetLabMode(labModeEnabled)

	lazyManager.StartMonitor(ctx)

	// 🎼 SSOT: 订阅 Orchestrator 状态快照，通过 WS 广播
	orchestrator := engine.GetOrchestrator()
	snapshotCh := orchestrator.Subscribe()
	go func() {
		for snapshot := range snapshotCh {
			// 将 CoordinatorState 转换为 WS 事件
			wsEvent := web.WSEvent{
				Type: "status_update",
				Data: map[string]interface{}{
					"latest_height": snapshot.LatestHeight,
					"synced_cursor": snapshot.SyncedCursor,
					"transfers":     snapshot.Transfers,
					"is_eco_mode":   snapshot.IsEcoMode,
					"progress":      snapshot.Progress,
					"system_state":  snapshot.SystemState.String(),
					"updated_at":    snapshot.UpdatedAt.Format(time.RFC3339),
					"sync_lag":      snapshot.LatestHeight - snapshot.SyncedCursor,
				},
			}
			wsHub.Broadcast(wsEvent)
		}
	}()

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
	initServices(ctx, sm, startBlock, lazyManager, rpcPool, wsHub)
}

func connectDB(ctx context.Context, isLocalAnvil bool) (*sqlx.DB, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := sqlx.ConnectContext(dbCtx, "pgx", cfg.DatabaseURL)
	if err != nil {
		slog.Error("❌ Database connection failed", "err", err)
		return nil, err
	}

	if isLocalAnvil {
		// 🔥 Anvil 实验室配置：激进连接池（无限火力）
		db.SetMaxOpenConns(100)                 // 无限火力
		db.SetMaxIdleConns(20)                  // 保持热连接
		db.SetConnMaxLifetime(30 * time.Minute) // 更长生命周期
		db.SetConnMaxIdleTime(5 * time.Minute)
		slog.Info("🔥 Anvil database pool: 100 max connections (Lab Mode)")
	} else {
		// 🛡️ 生产环境：保守配置（安全第一）
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)
		db.SetConnMaxIdleTime(1 * time.Minute)
		slog.Info("🛡️ Production database pool: 25 connections, safety first")
	}

	return db, nil
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

func initServices(ctx context.Context, sm *ServiceManager, startBlock *big.Int, lazyManager *engine.LazyManager, rpcPool engine.RPCClient, wsHub *web.Hub) {
	var wg sync.WaitGroup
	sm.fetcher.Start(ctx, &wg)
	sequencer := engine.NewSequencerWithFetcher(sm.Processor, sm.fetcher, startBlock, cfg.ChainID, sm.fetcher.Results, make(chan error, 100), nil, engine.GetMetrics())

	// 🔥 横滨实验室：设置 Sequencer 引用到 Fetcher（用于背压检测）
	sm.fetcher.SetSequencer(sequencer)
	slog.Info("🔥 Backpressure sensing enabled: Fetcher → Sequencer linked")

	// 🎼 SSOT: 初始化策略与协调器 (单一控制面)
	strategy := engine.GetStrategy(cfg.ChainID)
	orchestrator := engine.GetOrchestrator()
	orchestrator.Init(ctx, sm.fetcher, strategy)

	// 🚀 执行环境特定的启动逻辑 (自愈对齐)
	if err := strategy.OnStartup(ctx, orchestrator, sm.db, cfg.ChainID); err != nil {
		slog.Error("🎼 Orchestrator: Strategy startup failed", "err", err)
	}

	// 🔥 横滨实验室：初始化异步写入器 (Muscle)
	// 策略控制：如果 ShouldPersist=false，则进入全内存模式
	asyncWriter := engine.NewAsyncWriter(sm.Processor.GetDB(), orchestrator, !strategy.ShouldPersist(), cfg.ChainID)
	orchestrator.SetAsyncWriter(asyncWriter)
	asyncWriter.Start()
	slog.Info("🔥 AsyncWriter initialized", "persisting", strategy.ShouldPersist())

	// 🛡️ 初始化自愈审计引擎 (Immune System)
	healer := engine.NewSelfHealer(orchestrator)
	go healer.Start(ctx)
	slog.Info("🛡️ SelfHealer activated: Logic audit loop online")

	// 🛡️ Deadlock Watchdog: enabled for all networks (Anvil, Sepolia, production).
	// Enable() is now unconditional; the old chainID==31337 gate has been removed.
	watchdog := engine.NewDeadlockWatchdog(
		cfg.ChainID,
		cfg.DemoMode,
		sequencer,
		sm.Processor.GetRepoAdapter(),
		rpcPool,
		lazyManager,
		engine.GetMetrics(),
	)

	// Lower gap threshold for fast-block networks (Sepolia ~12s blocks).
	if cfg.ChainID == 11155111 {
		watchdog.SetGapThreshold(500)
	}

	watchdog.SetFetcher(sm.fetcher)
	watchdog.Enable()

	watchdog.OnHealingTriggered = func(event engine.HealingEvent) {
		wsHub.Broadcast(web.WSEvent{
			Type: "system_healing",
			Data: event,
		})
	}

	watchdog.Start(ctx)

	slog.Info("🛡️ DeadlockWatchdog initialized and started",
		"chain_id", cfg.ChainID,
		"demo_mode", cfg.DemoMode)

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

	// 🚀 工业级优化：本地 Anvil 实验室使用超高频轮询（100ms）
	tickerInterval := 500 * time.Millisecond
	// 🔥 横滨实验室滑动时间窗口：Anvil 环境下，调度更激进的范围
	schedulingWindow := big.NewInt(10) // 默认调度窗口：10 个块
	if cfg.ChainID == 31337 {
		tickerInterval = 100 * time.Millisecond
		schedulingWindow = big.NewInt(100) // Anvil：100 个块窗口
		slog.Info("🔥 Anvil TailFollow: 100ms hyper-frequency, 100-block sliding window")
	}
	ticker := time.NewTicker(tickerInterval)

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
			} else {
				// 🔥 SSOT: 通过 Orchestrator 更新链头（单一控制面）
				orch := engine.GetOrchestrator()
				orch.UpdateChainHead(tip.Uint64())

				// 🚀 获取考虑安全缓冲后的目标高度
				snap := orch.GetSnapshot()
				targetHeight := big.NewInt(int64(snap.TargetHeight))

				if targetHeight.Cmp(lastScheduled) > 0 {
					slog.Debug("🎼 [TailFollow] Chain head update dispatched", "tip", tip.String(), "target", targetHeight.String())

					// 🔥 滑动时间窗口批处理：调度 lastScheduled+1 到 targetHeight+schedulingWindow
					// 这确保了即便有安全垫，也能批量调度
					nextBlock := new(big.Int).Add(lastScheduled, big.NewInt(1))
					aggressiveTarget := new(big.Int).Add(targetHeight, schedulingWindow)
					if aggressiveTarget.Cmp(tip) > 0 {
						aggressiveTarget = new(big.Int).Set(tip)
					}
					if nextBlock.Cmp(aggressiveTarget) > 0 {
						continue
					}

					slog.Debug("🐕 [TailFollow] Scheduling new range",
						"from", nextBlock.String(),
						"to", aggressiveTarget.String(),
						"target", targetHeight.String(),
						"window", schedulingWindow.Int64())
					if err := fetcher.Schedule(ctx, nextBlock, aggressiveTarget); err != nil {
						slog.Error("🐕 [TailFollow] Failed to schedule", "err", err)
						continue
					}
					lastScheduled.Set(aggressiveTarget)
				}
			}
		}
	}
}