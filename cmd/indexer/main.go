package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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
	wssUrl := os.Getenv("WSS_URL")
	dbUrl := os.Getenv("DATABASE_URL")
	
	// 多 RPC 提供商支持：支持逗号分隔的 URL 列表
	// 格式：RPC_URLS=https://provider1/...,https://provider2/...
	rpcUrlList := strings.Split(rpcUrls, ",")
	for i := range rpcUrlList {
		rpcUrlList[i] = strings.TrimSpace(rpcUrlList[i])
	}
	
	if len(rpcUrlList) == 0 || rpcUrlList[0] == "" || dbUrl == "" {
		engine.Logger.Error("missing_required_env_vars",
			slog.String("error", "RPC_URLS and DATABASE_URL must be set in environment"),
		)
		os.Exit(1)
	}
	
	engine.Logger.Info("rpc_providers_configured",
		slog.Int("provider_count", len(rpcUrlList)),
		slog.String("providers", strings.Join(rpcUrlList, " | ")),
		slog.Bool("wss_available", wssUrl != ""),
	)

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
	
	// 初始化多节点RPC池（支持故障转移）
	rpcPool, err := engine.NewRPCClientPool(rpcUrlList)
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
	
	// 创建 HTTP server 用于优雅关闭
	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	
	// 在 goroutine 中启动 HTTP server
	go func() {
		engine.Logger.Info("http_server_started",
			slog.String("port", "8080"),
			slog.String("health_endpoint", "http://localhost:8080/healthz"),
			slog.String("metrics_endpoint", "http://localhost:8080/metrics"),
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			engine.Logger.Error("http_server_error",
				slog.String("error", err.Error()),
			)
		}
	}()
	
	// 致命错误通道 - 用于触发优雅关闭
	fatalErrCh := make(chan error, 1)
	
	// Reorg 事件通道 - 用于处理链重组
	reorgCh := make(chan engine.ReorgEvent, 1)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 4. 启动 Fetcher
	fetcher.Start(ctx, &wg)

	// 5. 从 checkpoint 恢复起始区块
	chainIDStr := os.Getenv("CHAIN_ID")
	if chainIDStr == "" {
		chainIDStr = "1" // 默认为以太坊主网
	}
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		engine.Logger.Error("invalid_chain_id",
			slog.String("chain_id", chainIDStr),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	
	// 优先从 sync_status 表恢复（持久性检查点），其次从 checkpoint 表
	startBlock, err := getStartBlockFromCheckpoint(ctx, db, chainID)
	if err != nil {
		engine.Logger.Error("checkpoint_recovery_failed",
			slog.String("error", err.Error()),
			slog.Int64("chain_id", chainID),
		)
		os.Exit(1)
	}
	
	engine.Logger.Info("checkpoint_recovered",
		slog.String("start_block", startBlock.String()),
		slog.Int64("chain_id", chainID),
	)
	
	// 6. 动态获取链上最新块高（支持增量同步）
	latestBlock, err := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		engine.Logger.Error("failed_to_get_latest_block",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	
	engine.Logger.Info("latest_block_fetched",
		slog.String("latest_block", latestBlock.String()),
		slog.String("start_block", startBlock.String()),
		slog.String("blocks_behind", new(big.Int).Sub(latestBlock, startBlock).String()),
	)
	
	// 调度任务：从 checkpoint 同步到最新块（支持增量同步）
	// 如果差距太大（>10000），分批同步以避免内存溢出
	batchSize := big.NewInt(1000)
	if new(big.Int).Sub(latestBlock, startBlock).Cmp(big.NewInt(10000)) > 0 {
		batchSize = big.NewInt(500) // 大差距时减小批次
	}
	
	endBlock := new(big.Int).Add(startBlock, batchSize)
	if endBlock.Cmp(latestBlock) > 0 {
		endBlock = new(big.Int).Set(latestBlock) // 不超过最新块
	}
	
	if err := fetcher.Schedule(ctx, startBlock, endBlock); err != nil {
		engine.Logger.Error("schedule_failed",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	engine.Logger.Info("blocks_scheduled",
		slog.String("start_block", startBlock.String()),
		slog.String("end_block", endBlock.String()),
		slog.String("mode", "incremental_sync"),
	)

	// 6. 启动 Sequencer - 确保顺序处理（传入 Fetcher 用于 Reorg 时暂停）
	sequencer := engine.NewSequencerWithFetcher(processor, fetcher, startBlock, chainID, fetcher.Results, fatalErrCh, reorgCh, metrics)
	
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
	
	for {
		select {
		case sig := <-sigCh:
			engine.Logger.Info("shutdown_signal_received",
				slog.String("signal", sig.String()),
			)
			goto shutdown
		case fatalErr := <-fatalErrCh:
			engine.Logger.Error("fatal_error_received",
				slog.String("error", fatalErr.Error()),
			)
			goto shutdown
		case reorgEvent := <-reorgCh:
			// 处理 reorg 事件：停止、回滚、重新调度
			engine.Logger.Warn("reorg_event_received",
				slog.String("at_block", reorgEvent.At.String()),
			)
			
			// 停止 fetcher 防止继续写入
			fetcher.Stop()
			
			// 计算共同祖先并回滚
			ancestorNum, err := processor.HandleDeepReorg(ctx, reorgEvent.At)
			if err != nil {
				engine.Logger.Error("reorg_handling_failed",
					slog.String("error", err.Error()),
				)
				goto shutdown
			}
			
			// 从祖先+1 重新调度
			resumeBlock := new(big.Int).Add(ancestorNum, big.NewInt(1))
			resumeEndBlock := new(big.Int).Add(resumeBlock, big.NewInt(100))
			
			// 创建新的 fetcher（旧的已停止）
			fetcher = engine.NewFetcher(rpcPool, 10)
			fetcher.Start(ctx, &wg)
			
			if err := fetcher.Schedule(ctx, resumeBlock, resumeEndBlock); err != nil {
				engine.Logger.Error("reorg_reschedule_failed",
					slog.String("error", err.Error()),
				)
				goto shutdown
			}
			
			engine.Logger.Info("reorg_recovery_complete",
				slog.String("resume_block", resumeBlock.String()),
				slog.String("resume_end_block", resumeEndBlock.String()),
			)
			// 继续循环等待下一个事件
		}
	}
	
shutdown:
	
	// 触发优雅关闭
	engine.Logger.Info("shutdown_initiated")
	
	// 优雅关闭 HTTP server（等待现有请求完成，最多 5 秒）
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		engine.Logger.Error("http_server_shutdown_error",
			slog.String("error", err.Error()),
		)
	}
	
	// 取消主 context，停止 fetcher 和 sequencer
	cancel()
	wg.Wait()
	
	engine.Logger.Info("shutdown_complete")
}
