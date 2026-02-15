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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/emulator"
	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/web"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	forceFrom         string // 强制起始块
)

func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, rpcPool engine.RPCClient, chainID int64, forceFrom string) (*big.Int, error) {
	// Priority 2: --start-from flag (highest runtime priority)
	if forceFrom != "" {
		if forceFrom == "latest" {
			latestChainBlock, err := rpcPool.GetLatestBlockNumber(ctx)
			if err != nil {
				slog.Error("failed_to_get_latest_block", "err", err)
				return big.NewInt(0), nil
			}
			slog.Info("🔄 FORCED_START", "from", "latest", "reason", "--start-from flag")
			return new(big.Int).Add(latestChainBlock, big.NewInt(1)), nil
		}

		// 尝试解析为区块号
		if blockNum, ok := new(big.Int).SetString(forceFrom, 10); ok {
			slog.Info("🔄 FORCED_START", "block", blockNum.String(), "reason", "--start-from flag")
			return blockNum, nil
		}

		slog.Warn("invalid_start_from_value", "value", forceFrom, "fallback", "checkpoint")
	}

	// Priority 3: START_BLOCK=latest config (highest config priority)
	// ✨ 实现"启动即瞬移"：从链头开始，带 Reorg 安全偏移
	if cfg.StartBlockStr == "latest" {
		latestChainBlock, err := rpcPool.GetLatestBlockNumber(ctx)
		if err != nil {
			slog.Error("failed_to_get_latest_block", "err", err)
			return big.NewInt(0), nil
		}

		// 🛡️ Reorg 安全：从链头倒数第 6 个块开始（约 72 秒缓冲）
		// 这确保了数据一致性，同时保持低延迟（< 2 分钟）
		reorgSafetyOffset := int64(6)
		startBlock := new(big.Int).Sub(latestChainBlock, big.NewInt(reorgSafetyOffset))

		// 确保不为负数
		if startBlock.Cmp(big.NewInt(0)) < 0 {
			startBlock = big.NewInt(0)
		}

		slog.Info("🚀 STARTING_FROM_LATEST_TELEPORT",
			"chain_head", latestChainBlock.String(),
			"start_block", startBlock.String(),
			"reorg_safety_offset", reorgSafetyOffset,
			"reason", "START_BLOCK=latest with Reorg protection (chain_head - 6)")

		// 检查是否有旧的检查点需要覆盖
		var lastSyncedBlock string
		err = db.GetContext(ctx, &lastSyncedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)

		if err == nil && lastSyncedBlock != "" {
			checkpointNum, _ := new(big.Int).SetString(lastSyncedBlock, 10)
			if checkpointNum.Cmp(startBlock) < 0 {
				slog.Info("🧹 OVERWRITING_OLD_CHECKPOINT",
					"old_checkpoint", checkpointNum.String(),
					"new_start", startBlock.String(),
					"reason", "START_BLOCK=latest overrides stale checkpoint")
			}
		}

		return startBlock, nil
	}

	// Priority 4: START_BLOCK=<number> config
	if cfg.StartBlock > 0 {
		slog.Info("🎯 STARTING_FROM_CONFIG", "block", cfg.StartBlock)
		return new(big.Int).SetInt64(cfg.StartBlock), nil
	}

	// Priority 5: Genesis hash validation (environment reset detection)
	rpcGenesis, err := rpcPool.BlockByNumber(ctx, big.NewInt(0))
	if err == nil && rpcGenesis != nil {
		var dbGenesisHash string
		err = db.GetContext(ctx, &dbGenesisHash, "SELECT hash FROM blocks WHERE number = 0")
		if err == nil && dbGenesisHash != rpcGenesis.Hash().Hex() {
			slog.Warn("🚨 DETECTED_ENVIRONMENT_RESET", "action", "wiping_stale_data")
			_, _ = db.ExecContext(ctx, "TRUNCATE TABLE blocks, transfers CASCADE")
			_, _ = db.ExecContext(ctx, "DELETE FROM sync_checkpoints")
			return big.NewInt(0), nil
		}
	}

	// Priority 6: Database checkpoint (default recovery behavior)
	var lastSyncedBlock string
	err = db.GetContext(ctx, &lastSyncedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)

	if err != nil || lastSyncedBlock == "" {
		slog.Info("🆕 NO_CHECKPOINT", "action", "starting_from_minimum")
		// 🛡️ 使用安全下限而非创世块
		return big.NewInt(10262444), nil
	}

	blockNum, _ := new(big.Int).SetString(lastSyncedBlock, 10)

	// 🛡️ 安全下限检查：检查点必须 >= 10262444
	minStartBlock := big.NewInt(10262444)
	if blockNum.Cmp(minStartBlock) < 0 {
		slog.Warn("🛡️ CHECKPOINT_BELOW_MINIMUM",
			"checkpoint_block", blockNum.String(),
			"minimum_block", minStartBlock.String(),
			"action", "using_minimum")
		return minStartBlock, nil
	}

	// 检查检查点漂移
	latestChainBlock, rpcErr := rpcPool.GetLatestBlockNumber(ctx)
	if rpcErr == nil && latestChainBlock != nil {
		if blockNum.Cmp(latestChainBlock) > 0 {
			slog.Warn("🚨 CHECKPOINT_DRIFT_DETECTED", "action", "cleaning_future_data")
			_, _ = db.ExecContext(ctx, "DELETE FROM blocks WHERE number > $1", latestChainBlock.String())
			_, _ = db.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1 WHERE chain_id = $2", latestChainBlock.String(), chainID)
			return latestChainBlock, nil
		}

		// 演示模式滑动窗口（仅在落后 > 5000 块时）
		if cfg.DemoMode {
			lag := new(big.Int).Sub(latestChainBlock, blockNum)
			if lag.Cmp(big.NewInt(5000)) > 0 {
				slog.Warn("⏩ JUMPING_TO_LATEST", "lag", lag.String(), "reason", "demo_mode_sliding_window")
				return new(big.Int).Sub(latestChainBlock, big.NewInt(1000)), nil
			}
		}
	}

	slog.Info("♻️ RESUMING_FROM_CHECKPOINT", "block", blockNum.String())
	return new(big.Int).Add(blockNum, big.NewInt(1)), nil
}

func main() {
	resetDB := flag.Bool("reset", false, "Reset database")
	startFrom := flag.String("start-from", "", "Force start from: 'latest' or specific block number")
	flag.Parse()
	cfg = config.Load()
	engine.InitLogger(cfg.LogLevel)

	// 保存命令行参数供后续使用
	forceFrom = *startFrom

	if cfg.DemoMode {
		for _, url := range cfg.RPCURLs {
			if !strings.Contains(url, "localhost") && !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "anvil") {
				slog.Error("🚫 SAFETY_LOCK: Local only in DemoMode (use localhost, 127.0.0.1, or anvil)")
				os.Exit(1)
			}
		}
	}

	// 🎬 演示模式：慢速可见的同步速度（适用于 testnet）
	if cfg.IsTestnet {
		slog.Info("🎬 DEMO_MODE_ENABLED", "settings", map[string]interface{}{
			"concurrency":     1,
			"qps":             3,
			"description":     "慢速人眼可见演示",
		})

		// 演示模式参数：并发 1，QPS 3
		cfg.FetchConcurrency = 1
		cfg.RPCRateLimit = 3
		cfg.MaxSyncBatch = 1

		// ✨ 实现"启动即瞬移"：START_BLOCK=latest 时从链头开始
		// 如果用户明确指定了数字起始块（如 START_BLOCK=10262444），则使用指定值
		// 如果 START_BLOCK=latest 或未设置，由 getStartBlockFromCheckpoint 动态解析
		// 移除硬编码下限，允许真正的"latest - N" 逻辑
		slog.Info("✨ STARTUP_TELEPORT_ENABLED",
			"logic", "START_BLOCK=latest will resolve to chain_head - 6 (Reorg safety)")
	}

	var db *sqlx.DB
	var err error
	db, err = sqlx.Connect("pgx", cfg.DatabaseURL)
	if err != nil {
		slog.Error("db_fail", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("db_close_fail", "err", err)
		}
	}()

	// 确保访问者统计表存在 (SRE 审计强化)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS visitor_stats (
		id SERIAL PRIMARY KEY,
		ip_address INET NOT NULL,
		user_agent TEXT,
		metadata JSONB NOT NULL,
		created_at TIMESTAMPTZ DEFAULT NOW()
	); CREATE INDEX IF NOT EXISTS idx_visitor_metadata ON visitor_stats USING GIN (metadata);`)
	if err != nil {
		slog.Error("create_table_fail", "err", err)
	}

	if *resetDB {
		_, err = db.Exec("TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;")
		if err != nil {
			slog.Error("reset_db_fail", "err", err)
		}
	} else {
		// 🚀 P1 Minor 优化：先验数据初始化 (Preheat Status)
		// 在演示时让数据显得更专业，启动前先从数据库同步最后一条记录
		var lastNum int64
		err := db.Get(&lastNum, "SELECT COALESCE(MAX(number), 0) FROM blocks")
		if err == nil && lastNum > 0 {
			engine.GetMetrics().UpdateCurrentSyncHeight(lastNum)
			slog.Info("🔥 Preheat Status: Initialized metrics from database", "latest_indexed", lastNum)
		}
	}

	var rpcPool engine.RPCClient
	if cfg.IsTestnet {
		// Use enhanced RPC pool for testnet with strict rate limiting
		rpcPool, err = engine.NewEnhancedRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.IsTestnet, cfg.MaxSyncBatch, cfg.RPCTimeout)
		if err != nil || rpcPool == nil {
			slog.Error("enhanced_rpc_fail")
			os.Exit(1)
		}
		slog.Info("Using enhanced RPC pool for testnet", "max_sync_batch", cfg.MaxSyncBatch)
	} else {
		// Use standard RPC pool for local/anvil
		rpcPool, err = engine.NewRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.RPCTimeout)
		if err != nil || rpcPool == nil {
			slog.Error("rpc_fail")
			os.Exit(1)
		}
		if standardPool, ok := rpcPool.(*engine.RPCClientPool); ok {
			standardPool.SetRateLimit(cfg.RPCRateLimit, cfg.RPCRateLimit*2)
		}
	}
	defer func() {
		if closer, ok := rpcPool.(interface{ Close() }); ok {
			closer.Close()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	sm := NewServiceManager(db, rpcPool, cfg.ChainID, cfg.RetryQueueSize)
	sm.processor.SetBatchCheckpoint(1)

	// Initialize LazyManager for controlling indexing based on demand
	// Using 3-minute cooldown and 3-minute active period
	lazyManager := engine.NewLazyManager(sm.fetcher, rpcPool, 3*time.Minute, 3*time.Minute)
	
	// Mode Selection: Energy Saving (Lazy) vs. Continuous Sync
	if cfg.EnableEnergySaving {
		// Start initial indexing for 60 seconds on startup, then pause (Lazy Mode)
		go func() {
			slog.Info("🔄 INITIALIZING LAZY INDEXER", "initial_duration", "60s")
			lazyManager.StartInitialIndexing()
			// After 60 seconds, pause indexing initially
			time.Sleep(60 * time.Second)
			lazyManager.DeactivateIndexingForced() 
			slog.Info("⏸️ INITIAL INDEXING COMPLETE", "state", "paused_until_triggered")
		}()
	} else {
		// Continuous Sync Mode: Always active
		slog.Info("⚡ CONTINUOUS_SYNC_MODE_ENABLED", "reason", "ENABLE_ENERGY_SAVING=false")
		lazyManager.StartInitialIndexing()
		// No forced deactivation, indexing will continue indefinitely
	}
	
	// Start the heartbeat mechanism to keep chain head updated even when paused
	lazyManager.StartHeartbeat(ctx, &DBWrapper{db}, cfg.ChainID)

	wsHub := web.NewHub()
	wg.Add(1)
	go func() { defer wg.Done(); wsHub.Run(ctx) }()

	sm.processor.EventHook = func(eventType string, data interface{}) {
		wsHub.Broadcast(web.WSEvent{Type: eventType, Data: data})
	}

	go startPerformanceMonitor(ctx)

	// 仿真器
	emuCfg := emulator.LoadConfig()
	if emuCfg.Enabled && len(cfg.RPCURLs) > 0 {
		slog.Info("🎰 Emulator enabled", "tx_interval", emuCfg.TxInterval.String(), "rpc", cfg.RPCURLs[0])
		emu, err := emulator.NewEmulator(cfg.RPCURLs[0], emuCfg.PrivateKey, emulator.WithTxInterval(emuCfg.TxInterval))
		if err != nil {
			slog.Error("❌ Emulator init failed", "err", err)
		} else {
			emu.OnSelfHealing = func(r string) {
				selfHealingEvents.Add(1)
				wsHub.Broadcast(web.WSEvent{
					Type: "log",
					Data: map[string]interface{}{
						"message": fmt.Sprintf("🛠️  Self-Healing: %s fixed", r),
						"level":   "warn",
					},
				})
			}
			wg.Add(1)
			go func() { defer wg.Done(); _ = emu.Start(ctx, nil) }()
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/static/", web.HandleStatic())
	mux.HandleFunc("/", web.RenderDashboard)
	mux.HandleFunc("/security", web.RenderSecurity)
	mux.HandleFunc("/ws", wsHub.HandleWS)
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { handleGetStatus(w, r, db, rpcPool, lazyManager, cfg.ChainID) })
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) { handleGetBlocks(w, r, db) })
	mux.HandleFunc("/api/transfers", func(w http.ResponseWriter, r *http.Request) { handleGetTransfers(w, r, db) })

	// 初始化 Ed25519 签名中间件
	signer, err := engine.NewSigningMiddleware(engine.GetORInitSeed(), "zw-web3-indexer-v1")
	if err != nil {
		slog.Error("signer_init_fail", "err", err)
	}
	signedHandler := signer.Handler(mux)

	// 应用访问者审计中间件 (SRE 增强)
	auditedHandler := VisitorStatsMiddleware(db, signedHandler)

	// Get port from environment variable, default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("🚀 Indexer API Server starting", "port", port)
	server := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           auditedHandler,
		ReadHeaderTimeout: 30 * time.Second,
	}
	go func() { _ = server.ListenAndServe() }()

	startBlock, err := sm.GetStartBlock(ctx, forceFrom)
	if err != nil || startBlock == nil {
		startBlock = big.NewInt(0)
	}
	sm.fetcher.Start(ctx, &wg)

	fatalErrCh := make(chan error, 1)
	sequencer := engine.NewSequencerWithFetcher(sm.processor, sm.fetcher, startBlock, cfg.ChainID, sm.fetcher.Results, fatalErrCh, nil, engine.GetMetrics())
	if sequencer == nil {
		slog.Error("🚫 FAILED_TO_INIT_SEQUENCER")
		os.Exit(1)
	}
	wg.Add(1)
	go func() { defer wg.Done(); sequencer.Run(ctx) }()
	go sm.StartTailFollow(ctx, startBlock)

	// 信号等待 (阻塞 main 协程)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	slog.Info("🏁 System Operational. Press Ctrl+C to stop.")
	<-sigCh

	slog.Warn("🛑 Shutdown signal received...")
	cancel()
	_ = server.Shutdown(context.Background())
	wg.Wait()
	slog.Info("✅ Shutdown complete")
}

// 补全 continuousTailFollow
func continuousTailFollow(ctx context.Context, fetcher *engine.Fetcher, rpcPool engine.RPCClient, startBlock *big.Int) {
	lastScheduled := new(big.Int).Sub(startBlock, big.NewInt(1))
	ticker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tip, err := rpcPool.GetLatestBlockNumber(ctx)
			if err == nil && tip.Cmp(lastScheduled) > 0 {
				_ = fetcher.Schedule(ctx, new(big.Int).Add(lastScheduled, big.NewInt(1)), tip)
				lastScheduled.Set(tip)
			}
		}
	}
}
