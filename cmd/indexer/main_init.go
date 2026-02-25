package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"web3-indexer-go/internal/database"
	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/recovery"
	"web3-indexer-go/internal/web"
	networkpkg "web3-indexer-go/pkg/network"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jmoiron/sqlx"
)

func initEngine(ctx context.Context, apiServer *Server, wsHub *web.Hub, resetDB bool) {
	slog.Info("⏳ Async engine initialization started...")
	recovery.OnPanic = func(name string, err interface{}, _ string) {
		wsHub.Broadcast(web.WSEvent{
			Type: "engine_panic",
			Data: map[string]interface{}{"worker": name, "error": fmt.Sprintf("%v", err), "ts": time.Now().Unix()},
		})
	}

	db, err := connectDB(ctx, cfg.ChainID == 31337)
	if err != nil {
		return
	}
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

	perfProfile := engine.GetPerformanceProfile(cfg.RPCURLs, cfg.ChainID)
	perfProfile.ApplyToConfig(cfg)

	tpsLimit := cfg.RPCRateLimit
	if perfProfile.EnableAggressiveBatch {
		tpsLimit = int(perfProfile.TPSLimit)
	}

	sm := NewServiceManager(db, rpcPool, cfg.ChainID, cfg.RetryQueueSize, tpsLimit, tpsLimit*2, cfg.FetchConcurrency, cfg.EnableSimulator, cfg.NetworkMode, cfg.EnableRecording, cfg.RecordingPath)
	sm.fetcher.SetThroughputLimit(float64(tpsLimit))

	if err := sm.Processor.AlignInfrastructure(ctx, rpcPool); err != nil {
		slog.Error("❌ [FATAL] Infrastructure alignment failed", "err", err)
	}

	guard := engine.NewConsistencyGuard(sm.Processor.GetRepoAdapter(), rpcPool)
	guard.SetDemoMode(cfg.DemoMode)
	guard.OnStatus = func(status string, detail string, progress int) {
		wsHub.Broadcast(web.WSEvent{Type: "linearity_status", Data: map[string]interface{}{"status": status, "detail": detail, "progress": progress}})
	}
	if err := guard.PerformLinearityCheck(ctx); err != nil {
		slog.Error("linearity_check_failed", "err", err)
	}

	engine.GetHeightOracle().StrictHeightCheck = cfg.StrictHeightCheck
	engine.GetHeightOracle().DriftTolerance = cfg.DriftTolerance

	lazyManager := engine.NewLazyManager(sm.fetcher, rpcPool, 5*time.Minute, guard)
	sm.lazyManager = lazyManager

	if cfg.ChainID == 31337 || engine.IsLocalAnvil(cfg.RPCURLs[0]) {
		lazyManager.SetAlwaysActive(true)
		sm.fetcher.SetThroughputLimit(100000.0)
	}

	engine.GetMetrics().SetLabMode(cfg.ChainID == 31337 || cfg.ForceAlwaysActive)
	lazyManager.StartMonitor(ctx)

	setupSubscriptions(wsHub)
	apiServer.SetDependencies(db, rpcPool, lazyManager, sm.Processor, cfg.ChainID)

	wsHub.OnActivity = func() { lazyManager.Trigger() }
	wsHub.OnNeedMeta = func(addr string) { sm.Processor.GetSymbol(common.HexToAddress(addr)) }

	sm.Processor.EventHook = func(eventType string, data interface{}) {
		wsHub.Broadcast(web.WSEvent{Type: eventType, Data: data})
	}

	startBlock, err := sm.GetStartBlock(ctx, forceFrom, resetDB)
	if err != nil {
		slog.Error("❌ Failed to determine start block", "err", err)
		startBlock = big.NewInt(cfg.StartBlock)
	}
	setupParentAnchor(ctx, db, rpcPool, startBlock)
	initServices(ctx, sm, startBlock, lazyManager, rpcPool, wsHub)
}

func setupSubscriptions(wsHub *web.Hub) {
	orchestrator := engine.GetOrchestrator()
	snapshotCh := orchestrator.Subscribe()
	go func() {
		for snapshot := range snapshotCh {
			wsHub.Broadcast(web.WSEvent{
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
			})
		}
	}()
}

func connectDB(ctx context.Context, isLocalAnvil bool) (*sqlx.DB, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := sqlx.ConnectContext(dbCtx, "pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if isLocalAnvil {
		db.SetMaxOpenConns(100)
		db.SetMaxIdleConns(20)
	} else {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)
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
	return verifyErr
}
