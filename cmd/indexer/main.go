package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"web3-indexer-go/internal/engine"

	_ "github.com/jackc/pgx/v5/stdlib" // PGX Driver
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
)

// getStartBlockFromCheckpoint 从数据库获取起始区块号
func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, chainID int64) (*big.Int, error) {
	var lastSyncedBlock string
	err := db.GetContext(ctx, &lastSyncedBlock, 
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
	
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			// 没有checkpoint，从0开始
			engine.Logger.Info("no_checkpoint_found",
				slog.Int64("chain_id", chainID),
				slog.String("action", "starting_from_block_0"),
			)
			return big.NewInt(0), nil
		}
		return nil, err
	}
	
	// 解析区块号
	blockNum, ok := new(big.Int).SetString(lastSyncedBlock, 10)
	if !ok {
		return nil, fmt.Errorf("invalid block number in checkpoint: %s", lastSyncedBlock)
	}
	
	// 从下一个区块开始
	startBlock := new(big.Int).Add(blockNum, big.NewInt(1))
	engine.LogCheckpointResumed(blockNum.String(), startBlock.String())
	
	return startBlock, nil
}

func main() {
	// 初始化结构化日志
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	engine.InitLogger(logLevel)
	
	engine.Logger.Info("starting_web3_indexer",
		slog.String("version", "V2"),
		slog.String("mode", "production_ready"),
	)
	
	// 1. 加载配置
	_ = godotenv.Load()
	rpcUrls := os.Getenv("RPC_URLS")
	dbUrl := os.Getenv("DATABASE_URL")
	
	if rpcUrls == "" || dbUrl == "" {
		engine.Logger.Error("missing_required_env_vars",
			slog.String("error", "RPC_URLS and DATABASE_URL must be set in environment"),
		)
		os.Exit(1)
	}

	// 2. 连接资源
	db, err := sqlx.Connect("pgx", dbUrl)
	if err != nil {
		engine.Logger.Error("database_connection_failed",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	defer db.Close()
	
	// 配置数据库连接池
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	engine.Logger.Info("database_connected",
		slog.Int("max_open_conns", 25),
		slog.Int("max_idle_conns", 10),
	)
	
	// Initialize metrics
	metrics := engine.GetMetrics()
	metrics.RecordStartTime()
	
	// 初始化多节点RPC池
	rpcPool, err := engine.NewRPCClientPool(strings.Split(rpcUrls, ","))
	if err != nil {
		engine.Logger.Error("rpc_pool_init_failed",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	defer rpcPool.Close()
	healthyNodes := rpcPool.GetHealthyNodeCount()
	engine.Logger.Info("rpc_pool_initialized",
		slog.Int("healthy_nodes", healthyNodes),
		slog.Int("total_urls", len(strings.Split(rpcUrls, ","))),
	)

	// 3. 初始化组件
	fetcher := engine.NewFetcher(rpcPool, 10) // 10 workers, 100 rps limit
	processor := engine.NewProcessor(db, rpcPool) // 传入RPC池用于reorg恢复
	
	// Start HTTP server with health checks and metrics
	mux := http.NewServeMux()
	
	// Initialize health server (pass nil for sequencer, will be updated later)
	healthServer := engine.NewHealthServer(db, rpcPool, nil, fetcher)
	healthServer.RegisterRoutes(mux)
	
	// Start Prometheus metrics server
	mux.Handle("/metrics", promhttp.Handler())
	
	go func() {
		engine.Logger.Info("http_server_started",
			slog.String("port", "8080"),
			slog.String("health_endpoint", "http://localhost:8080/healthz"),
			slog.String("metrics_endpoint", "http://localhost:8080/metrics"),
		)
		if err := http.ListenAndServe(":8080", mux); err != nil {
			engine.Logger.Error("http_server_error",
				slog.String("error", err.Error()),
			)
		}
	}()
	
	// 致命错误通道 - 用于触发优雅关闭
	fatalErrCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 4. 启动 Fetcher
	fetcher.Start(ctx, &wg)

	// 5. 从 checkpoint 恢复起始区块
	chainID := int64(1) // TODO: 从环境变量读取
	startBlock, err := getStartBlockFromCheckpoint(ctx, db, chainID)
	if err != nil {
		engine.Logger.Error("checkpoint_recovery_failed",
			slog.String("error", err.Error()),
			slog.Int64("chain_id", chainID),
		)
		os.Exit(1)
	}
	
	// 调度任务 (从 checkpoint 开始同步 100 个块用于演示)
	endBlock := new(big.Int).Add(startBlock, big.NewInt(100))
	if err := fetcher.Schedule(ctx, startBlock, endBlock); err != nil {
		engine.Logger.Error("schedule_failed",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	engine.Logger.Info("blocks_scheduled",
		slog.String("start_block", startBlock.String()),
		slog.String("end_block", endBlock.String()),
		slog.String("mode", "resumed_from_checkpoint"),
	)

	// 6. 启动 Sequencer - 确保顺序处理（传入 Fetcher 用于 Reorg 时暂停）
	sequencer := engine.NewSequencerWithFetcher(processor, fetcher, startBlock, 1, fetcher.Results, fatalErrCh, metrics)
	
	// 把 sequencer 注入到 healthServer（使健康检查能正确报告状态）
	healthServer.SetSequencer(sequencer)
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		sequencer.Run(ctx)
	}()
	
	engine.Logger.Info("sequencer_started",
		slog.String("mode", "ordered_processing"),
		slog.String("expected_block", startBlock.String()),
	)

	// 7. 优雅退出处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	select {
	case sig := <-sigCh:
		engine.Logger.Info("shutdown_signal_received",
			slog.String("signal", sig.String()),
		)
	case fatalErr := <-fatalErrCh:
		engine.Logger.Error("fatal_error_received",
			slog.String("error", fatalErr.Error()),
		)
	}
	
	// 触发优雅关闭
	engine.Logger.Info("shutdown_initiated")
	cancel()
	wg.Wait()
	engine.Logger.Info("shutdown_complete")
}
