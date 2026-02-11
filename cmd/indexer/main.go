package main

import (
	"context"
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

var (
	cfg               *config.Config
	selfHealingEvents atomic.Uint64
)

func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, rpcPool *engine.RPCClientPool, chainID int64) (*big.Int, error) {
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

	var lastSyncedBlock string
	err = db.GetContext(ctx, &lastSyncedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)

	latestChainBlock, rpcErr := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil || lastSyncedBlock == "" {
		return big.NewInt(0), nil
	}

	blockNum, _ := new(big.Int).SetString(lastSyncedBlock, 10)
	if rpcErr == nil && latestChainBlock != nil {
		if blockNum.Cmp(latestChainBlock) > 0 {
			slog.Warn("🚨 CHECKPOINT_DRIFT_DETECTED", "action", "cleaning_future_data")
			_, _ = db.ExecContext(ctx, "DELETE FROM blocks WHERE number > $1", latestChainBlock.String())
			_, _ = db.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1 WHERE chain_id = $2", latestChainBlock.String(), chainID)
			return latestChainBlock, nil
		}
		// 演示模式滑动窗口
		lag := new(big.Int).Sub(latestChainBlock, blockNum)
		if lag.Cmp(big.NewInt(5000)) > 0 {
			slog.Warn("⏩ JUMPING_TO_LATEST", "lag", lag.String())
			return new(big.Int).Sub(latestChainBlock, big.NewInt(1000)), nil
		}
	}
	return new(big.Int).Add(blockNum, big.NewInt(1)), nil
}

func main() {
	resetDB := flag.Bool("reset", false, "Reset database")
	flag.Parse()
	cfg = config.Load()
	engine.InitLogger(cfg.LogLevel)

	if cfg.DemoMode {
		for _, url := range cfg.RPCURLs {
			if !strings.Contains(url, "localhost") && !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "anvil") {
				slog.Error("🚫 SAFETY_LOCK: Local only in DemoMode (use localhost, 127.0.0.1, or anvil)")
				os.Exit(1)
			}
		}
	}

	db, err := sqlx.Connect("pgx", cfg.DatabaseURL)
	if err != nil {
		slog.Error("db_fail", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if *resetDB {
		_, _ = db.Exec("TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;")
	}

	rpcPool, err := engine.NewRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.RPCTimeout)
	if err != nil || rpcPool == nil {
		slog.Error("rpc_fail")
		os.Exit(1)
	}
	rpcPool.SetRateLimit(cfg.RPCRateLimit, cfg.RPCRateLimit*2)
	defer rpcPool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	sm := NewServiceManager(db, rpcPool, cfg.ChainID, cfg.RetryQueueSize)
	sm.processor.SetBatchCheckpoint(1)

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
						"level": "warn",
					},
				})
			}
			wg.Add(1)
			go func() { defer wg.Done(); emu.Start(ctx, nil) }()
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/static/", web.HandleStatic())
	mux.HandleFunc("/", web.RenderDashboard)
	mux.HandleFunc("/security", web.RenderSecurity)
	mux.HandleFunc("/ws", wsHub.HandleWS)
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { handleGetStatus(w, r, db, rpcPool) })
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) { handleGetBlocks(w, r, db) })
	mux.HandleFunc("/api/transfers", func(w http.ResponseWriter, r *http.Request) { handleGetTransfers(w, r, db) })

	// 初始化 Ed25519 签名中间件
	signer, _ := engine.NewSigningMiddleware(engine.GetORInitSeed(), "zw-web3-indexer-v1")
	signedHandler := signer.Handler(mux)

	server := &http.Server{Addr: "0.0.0.0:8080", Handler: signedHandler}
	go server.ListenAndServe()

	startBlock, err := sm.GetStartBlock(ctx)
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
	server.Shutdown(context.Background())
	wg.Wait()
	slog.Info("✅ Shutdown complete")
}

// 补全 continuousTailFollow
func continuousTailFollow(ctx context.Context, fetcher *engine.Fetcher, rpcPool *engine.RPCClientPool, startBlock *big.Int) {
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
