package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/engine"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "github.com/jackc/pgx/v5/stdlib" // PGX Driver
	"github.com/jmoiron/sqlx"
)

var cfg *config.Config

// IndexerServiceWrapper 实现IndexerService接口
type IndexerServiceWrapper struct {
	fetcher   *engine.Fetcher
	sequencer *engine.Sequencer
	ctx       context.Context
	wg        *sync.WaitGroup
	running   bool
	mu        sync.RWMutex
}

func (w *IndexerServiceWrapper) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return fmt.Errorf("indexer already running")
	}

	// 启动 fetcher
	w.fetcher.Start(ctx, w.wg)

	// 启动 sequencer
	if w.sequencer != nil {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.sequencer.Run(ctx)
		}()
	}

	w.running = true
	engine.Logger.Info("indexer_service_started")
	return nil
}

func (w *IndexerServiceWrapper) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return fmt.Errorf("indexer not running")
	}

	// 停止 fetcher
	w.fetcher.Stop()

	w.running = false
	engine.Logger.Info("indexer_service_stopped")
	return nil
}

func (w *IndexerServiceWrapper) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

func (w *IndexerServiceWrapper) GetCurrentBlock() string {
	if w.sequencer != nil {
		return w.sequencer.GetExpectedBlock().String()
	}
	return "unknown"
}

func (w *IndexerServiceWrapper) SetSequencer(sequencer *engine.Sequencer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sequencer = sequencer
}

// checkPortAvailable 检查端口是否可用
func checkPortAvailable(port int) error {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	listener.Close()
	return nil
}

// maskDatabaseURL 隐藏数据库URL中的敏感信息
func maskDatabaseURL(url string) string {
	if len(url) > 20 {
		return url[:10] + "***" + url[len(url)-10:]
	}
	return "***"
}

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
	// 1. 加载配置
	cfg = config.Load()

	// 初始化结构化日志
	engine.InitLogger(cfg.LogLevel)

	engine.Logger.Info("starting_web3_indexer",
		slog.String("version", "V2"),
		slog.String("mode", "production_ready"),
		slog.Int("rpc_providers_count", len(cfg.RPCURLs)),
		slog.Bool("wss_available", cfg.WSSURL != ""),
	)

	// 配置验证
	if len(cfg.RPCURLs) == 0 {
		engine.Logger.Error("no_rpc_urls_configured")
		os.Exit(1)
	}
	if cfg.ChainID <= 0 {
		engine.Logger.Error("invalid_chain_id", slog.Int64("chain_id", cfg.ChainID))
		os.Exit(1)
	}

	// 打印关键配置
	engine.Logger.Info("configuration_loaded",
		slog.Int("rpc_providers_count", len(cfg.RPCURLs)),
		slog.String("database_url", maskDatabaseURL(cfg.DatabaseURL)),
		slog.Int64("chain_id", cfg.ChainID),
		slog.Int64("start_block", cfg.StartBlock),
		slog.String("log_level", cfg.LogLevel),
		slog.Bool("wss_available", cfg.WSSURL != ""),
		slog.Duration("rpc_timeout", cfg.RPCTimeout),
	)

	// 2. 连接资源
	db, err := sqlx.Connect("pgx", cfg.DatabaseURL)
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
	rpcPool, err := engine.NewRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.RPCTimeout)
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
		slog.Int("total_urls", len(cfg.RPCURLs)),
		slog.Duration("timeout", cfg.RPCTimeout),
	)

	// 3. 初始化组件
	fetcher := engine.NewFetcher(rpcPool, 10)     // 10 workers, 100 rps limit
	processor := engine.NewProcessor(db, rpcPool) // 传入RPC池用于reorg恢复

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 致命错误通道 - 用于触发优雅关闭
	fatalErrCh := make(chan error, 1)

	// Reorg 事件通道 - 用于处理链重组
	reorgCh := make(chan engine.ReorgEvent, 1)

	// Start HTTP server with health checks and metrics
	mux := http.NewServeMux()

	// Initialize health server (pass nil for sequencer, will be updated later)
	healthServer := engine.NewHealthServer(db, rpcPool, nil, fetcher)
	healthServer.RegisterRoutes(mux)

	// 创建索引器服务包装器（实现IndexerService接口）
	indexerService := &IndexerServiceWrapper{
		fetcher:   fetcher,
		sequencer: nil, // 将在后面设置
		ctx:       ctx,
		wg:        &wg,
	}

	// 初始化状态管理器
	stateManager := engine.NewStateManager(indexerService, rpcPool)

	// 初始化管理员服务器
	adminServer := engine.NewAdminServer(stateManager)
	adminServer.RegisterRoutes(mux)

	// 注册静态文件服务（Dashboard）
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "internal/web/dashboard.html")
			stateManager.RecordAccess() // 记录Dashboard访问
		} else {
			http.NotFound(w, r)
		}
	})

	// 为所有API端点添加访问记录中间件
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		stateManager.RecordAccess() // 记录API访问
		// 继续处理请求
		if r.URL.Path == "/api/admin/start-demo" {
			adminServer.StartDemo(w, r)
		} else if r.URL.Path == "/api/admin/stop" {
			adminServer.Stop(w, r)
		} else if r.URL.Path == "/api/admin/status" {
			adminServer.GetStatus(w, r)
		} else if r.URL.Path == "/api/admin/config" {
			adminServer.GetConfig(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// Start Prometheus metrics server
	mux.Handle("/metrics", promhttp.Handler())

	// 创建 HTTP server 用于优雅关闭
	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// 检查端口冲突
	if err := checkPortAvailable(8080); err != nil {
		engine.Logger.Error("port_conflict",
			slog.Int("port", 8080),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	// 在 goroutine 中启动 HTTP server
	go func() {
		engine.Logger.Info("http_server_started",
			slog.String("port", "8080"),
			slog.String("health_endpoint", "http://localhost:8080/healthz"),
			slog.String("metrics_endpoint", "http://localhost:8080/metrics"),
			slog.String("dashboard_url", "http://localhost:8080"),
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			engine.Logger.Error("http_server_error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// 4. 启动 Fetcher
	fetcher.Start(ctx, &wg)

	// 5. 从 checkpoint 恢复起始区块
	chainID := cfg.ChainID

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

	// 设置sequencer到wrapper和状态管理器
	indexerService.SetSequencer(sequencer)

	// 启动状态管理器（智能休眠系统）
	stateManager.Start(ctx)

	engine.Logger.Info("smart_sleep_system_enabled",
		slog.Duration("demo_duration", 5*time.Minute),
		slog.Duration("idle_timeout", 10*time.Minute),
		slog.String("dashboard_url", "http://localhost:8080"),
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

	// 停止状态管理器
	stateManager.Stop()

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
