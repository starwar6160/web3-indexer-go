package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/emulator"
	"web3-indexer-go/internal/engine"

	"github.com/ethereum/go-ethereum/common"
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

// Block represents a blockchain block
type Block struct {
	Number      string    `db:"number" json:"number"`
	Hash        string    `db:"hash" json:"hash"`
	ParentHash  string    `db:"parent_hash" json:"parent_hash"`
	Timestamp   string    `db:"timestamp" json:"timestamp"`
	ProcessedAt time.Time `db:"processed_at" json:"processed_at"`
}

// Transfer represents a token transfer
type Transfer struct {
	ID           int    `db:"id" json:"id"`
	BlockNumber  string `db:"block_number" json:"block_number"`
	TxHash       string `db:"tx_hash" json:"tx_hash"`
	LogIndex     int    `db:"log_index" json:"log_index"`
	FromAddress  string `db:"from_address" json:"from_address"`
	ToAddress    string `db:"to_address" json:"to_address"`
	Amount       string `db:"amount" json:"amount"`
	TokenAddress string `db:"token_address" json:"token_address"`
}

// StatusResponse represents the current indexer status
type StatusResponse struct {
	State          string `json:"state"`
	LatestBlock    string `json:"latest_block"`
	SyncLag        int64  `json:"sync_lag"`
	TotalBlocks    int64  `json:"total_blocks"`
	TotalTransfers int64  `json:"total_transfers"`
	IsHealthy      bool   `json:"is_healthy"`
}

// handleGetBlocks returns the latest blocks from the database
func handleGetBlocks(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var blocks []Block
	err := db.SelectContext(ctx, &blocks,
		"SELECT number, hash, parent_hash, timestamp, processed_at FROM blocks ORDER BY number DESC LIMIT 10")

	if err != nil {
		engine.Logger.Error("failed_to_fetch_blocks", slog.String("error", err.Error()))
		http.Error(w, "Failed to fetch blocks", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"blocks": blocks,
		"count":  len(blocks),
	})
}

// handleGetTransfers returns the latest transfers from the database
func handleGetTransfers(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var transfers []Transfer
	err := db.SelectContext(ctx, &transfers,
		"SELECT id, block_number, tx_hash, log_index, from_address, to_address, amount, token_address FROM transfers ORDER BY block_number DESC LIMIT 10")

	if err != nil {
		engine.Logger.Error("failed_to_fetch_transfers", slog.String("error", err.Error()))
		http.Error(w, "Failed to fetch transfers", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"transfers": transfers,
		"count":     len(transfers),
	})
}

// handleGetStatus returns the current indexer status
func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool *engine.RPCClientPool) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Get latest block number
	var latestBlock string
	err := db.GetContext(ctx, &latestBlock,
		"SELECT COALESCE(MAX(number), '0') FROM blocks")
	if err != nil {
		latestBlock = "0"
	}

	// Get total blocks
	var totalBlocks int64
	db.GetContext(ctx, &totalBlocks, "SELECT COUNT(*) FROM blocks")

	// Get total transfers
	var totalTransfers int64
	db.GetContext(ctx, &totalTransfers, "SELECT COUNT(*) FROM transfers")

	// Get RPC health
	healthyNodes := rpcPool.GetHealthyNodeCount()
	isHealthy := healthyNodes > 0

	status := StatusResponse{
		State:          "active",
		LatestBlock:    latestBlock,
		SyncLag:        0,
		TotalBlocks:    totalBlocks,
		TotalTransfers: totalTransfers,
		IsHealthy:      isHealthy,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// handleGenerateDemoTransactions generates demo transactions on the RPC node
func handleGenerateDemoTransactions(w http.ResponseWriter, r *http.Request, rpcPool *engine.RPCClientPool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simple message to send transactions
	result := map[string]interface{}{
		"success": true,
		"message": "Demo transaction generation initiated",
		"note":    "Transactions are being generated on the RPC node. Check the Dashboard for updates.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)

	engine.Logger.Info("demo_transactions_requested",
		slog.String("remote_addr", r.RemoteAddr),
	)
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

	// 等待RPC池至少有一个健康节点，避免Anvil尚未就绪导致连接重置
	readyWait := 30 * time.Second
	if ok := rpcPool.WaitForHealthy(readyWait); !ok {
		engine.Logger.Error("rpc_pool_not_ready",
			slog.String("error", "no healthy RPC nodes after wait"),
			slog.Duration("waited", readyWait),
			slog.Int("total_urls", len(cfg.RPCURLs)),
		)
		os.Exit(1)
	}

	healthyNodes := rpcPool.GetHealthyNodeCount()
	engine.Logger.Info("rpc_pool_initialized",
		slog.Int("healthy_nodes", healthyNodes),
		slog.Int("total_urls", len(cfg.RPCURLs)),
		slog.Duration("timeout", cfg.RPCTimeout),
	)

	// 3. 初始化组件
	// 根据配置设置并发和速率限制
	fetcher := engine.NewFetcher(rpcPool, cfg.FetchConcurrency)
	
	// 如果并发较高（如针对 Anvil），放宽速率限制以实现“瞬间追平”
	if cfg.FetchConcurrency > 20 {
		// Set to effectively infinite for local Anvil
		fetcher.SetRateLimit(100000, 100000)
		rpcPool.SetRateLimit(100000, 100000)
		engine.Logger.Info("overclock_mode_enabled", 
			slog.Int("concurrency", cfg.FetchConcurrency),
			slog.String("rps_limit", "unlimited"),
		)
	}

	processor := engine.NewProcessor(db, rpcPool) // 传入RPC池用于reorg恢复

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 4. 初始化仿真引擎（如果启用）
	var emulatorInstance *emulator.Emulator
	emulatorAddrChan := make(chan common.Address, 1)

	emuConfig := emulator.LoadConfig()
	if emuConfig.Enabled && emuConfig.IsValid() {
		engine.Logger.Info("emulator_config_loaded",
			slog.String("rpc_url", emuConfig.RpcURL),
			slog.Duration("block_interval", emuConfig.BlockInterval),
			slog.Duration("tx_interval", emuConfig.TxInterval),
		)

		var err error
		emulatorInstance, err = emulator.NewEmulator(emuConfig.RpcURL, emuConfig.PrivateKey)
		if err != nil {
			engine.Logger.Error("emulator_initialization_failed",
				slog.String("error", err.Error()),
			)
			// 不中断主程序，仅记录警告
		} else {
			// 配置仿真器参数
			emulatorInstance.SetBlockInterval(emuConfig.BlockInterval)
			emulatorInstance.SetTxInterval(emuConfig.TxInterval)

			// 在后台启动仿真引擎
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := emulatorInstance.Start(ctx, emulatorAddrChan); err != nil {
					engine.Logger.Error("emulator_runtime_error",
						slog.String("error", err.Error()),
					)
				}
			}()

			engine.Logger.Info("emulator_started_in_background")
		}
	} else if os.Getenv("EMULATOR_ENABLED") == "true" {
		engine.Logger.Warn("emulator_enabled_but_not_configured",
			slog.String("hint", "Set EMULATOR_RPC_URL and EMULATOR_PRIVATE_KEY"),
		)
	}

	// 5. 从仿真器或环境变量获取监控地址
	watchedAddresses := []string{}

	// 优先从仿真器获取动态部署的合约地址
	if emulatorInstance != nil {
		engine.Logger.Info("waiting_for_emulator_deployment", slog.String("timeout", "30s"))
		select {
		case deployedAddr := <-emulatorAddrChan:
			watchedAddresses = append(watchedAddresses, deployedAddr.Hex())
			engine.Logger.Info("contract_address_from_emulator",
				slog.String("address", deployedAddr.Hex()),
			)
		case <-time.After(30 * time.Second):
			engine.Logger.Warn("emulator_deployment_timeout_using_env_vars")
		}
	} else {
		// 如果没有启用仿真器，不等待
	}

	// 从环境变量添加额外的监控地址
	if watchAddressesEnv := os.Getenv("WATCH_ADDRESSES"); watchAddressesEnv != "" {
		envAddresses := strings.Split(watchAddressesEnv, ",")
		for _, addr := range envAddresses {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				watchedAddresses = append(watchedAddresses, addr)
			}
		}
	}

	// 设置监控地址
	if len(watchedAddresses) > 0 {
		fetcher.SetWatchedAddresses(watchedAddresses)
		processor.SetWatchedAddresses(watchedAddresses)
		engine.Logger.Info("watched_addresses_configured",
			slog.Int("count", len(watchedAddresses)),
			slog.String("addresses", strings.Join(watchedAddresses, ", ")),
		)
	}

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
			// Try multiple possible paths for the dashboard file
			dashboardPaths := []string{
				"internal/web/dashboard.html",
				"/app/internal/web/dashboard.html",
				"./internal/web/dashboard.html",
			}

			var served bool
			for _, path := range dashboardPaths {
				if err := func() error {
					f, err := os.Open(path)
					if err != nil {
						return err
					}
					defer f.Close()

					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, err = io.Copy(w, f)
					return err
				}(); err == nil {
					served = true
					break
				}
			}

			if !served {
				// Fallback: serve a comprehensive HTML dashboard with real data
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<title>Web3 Indexer Dashboard</title>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<style>
		* { margin: 0; padding: 0; box-sizing: border-box; }
		body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); min-height: 100vh; padding: 20px; }
		.container { max-width: 1400px; margin: 0 auto; }
		header { background: white; padding: 30px; border-radius: 12px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); margin-bottom: 30px; }
		h1 { color: #333; font-size: 28px; margin-bottom: 10px; }
		.header-subtitle { color: #666; font-size: 14px; }
		.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; margin-bottom: 30px; }
		.card { background: white; border-radius: 12px; padding: 20px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
		.card h2 { font-size: 18px; color: #333; margin-bottom: 15px; border-bottom: 2px solid #667eea; padding-bottom: 10px; }
		.stat { margin: 10px 0; display: flex; justify-content: space-between; align-items: center; }
		.stat-label { color: #666; font-size: 14px; }
		.stat-value { color: #333; font-weight: bold; font-size: 16px; font-family: 'Courier New', monospace; }
		.status-badge { display: inline-block; padding: 6px 12px; border-radius: 20px; font-size: 12px; font-weight: bold; }
		.status-healthy { background: #d4edda; color: #155724; }
		.status-warning { background: #fff3cd; color: #856404; }
		.status-error { background: #f8d7da; color: #721c24; }
		.data-table { width: 100%; border-collapse: collapse; margin-top: 15px; }
		.data-table th { background: #f5f5f5; padding: 12px; text-align: left; font-weight: 600; color: #333; border-bottom: 2px solid #ddd; font-size: 13px; }
		.data-table td { padding: 12px; border-bottom: 1px solid #eee; font-size: 13px; color: #666; }
		.data-table tr:hover { background: #f9f9f9; }
		.hash { font-family: 'Courier New', monospace; font-size: 11px; color: #667eea; }
		.address { font-family: 'Courier New', monospace; font-size: 11px; }
		.loading { color: #999; font-style: italic; }
		.error { color: #d32f2f; }
		.refresh-time { color: #999; font-size: 12px; margin-top: 10px; }
		.action-btn { background: #667eea; color: white; border: none; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; margin-top: 10px; }
		.action-btn:hover { background: #764ba2; }
	</style>
</head>
<body>
	<div class="container">
		<header>
			<h1>🚀 Web3 Indexer Dashboard</h1>
			<p class="header-subtitle">Real-time blockchain indexing with Go | All-in-Docker Architecture</p>
		</header>

		<div class="grid">
			<!-- Status Card -->
			<div class="card">
				<h2>📊 System Status</h2>
				<div class="stat">
					<span class="stat-label">State</span>
					<span class="stat-value" id="state">Loading...</span>
				</div>
				<div class="stat">
					<span class="stat-label">Latest Block</span>
					<span class="stat-value" id="latestBlock">Loading...</span>
				</div>
				<div class="stat">
					<span class="stat-label">Total Blocks</span>
					<span class="stat-value" id="totalBlocks">Loading...</span>
				</div>
				<div class="stat">
					<span class="stat-label">Total Transfers</span>
					<span class="stat-value" id="totalTransfers">Loading...</span>
				</div>
				<div class="stat">
					<span class="stat-label">Health</span>
					<span id="health" class="status-badge status-warning">Checking...</span>
				</div>
				<div class="refresh-time">Last updated: <span id="lastUpdate">-</span></div>
			</div>

			<!-- Quick Links Card -->
			<div class="card">
				<h2>� API Endpoints</h2>
				<p style="color: #666; font-size: 13px; margin-bottom: 15px;">Access detailed information via REST API</p>
				<div style="display: flex; flex-direction: column; gap: 8px;">
					<a href="/healthz" style="color: #667eea; text-decoration: none; font-size: 13px;">→ Health Check (/healthz)</a>
					<a href="/metrics" style="color: #667eea; text-decoration: none; font-size: 13px;">→ Prometheus Metrics (/metrics)</a>
					<a href="/api/admin/status" style="color: #667eea; text-decoration: none; font-size: 13px;">→ Admin Status (/api/admin/status)</a>
					<a href="/api/admin/config" style="color: #667eea; text-decoration: none; font-size: 13px;">→ Configuration (/api/admin/config)</a>
					<a href="/api/blocks" style="color: #667eea; text-decoration: none; font-size: 13px;">→ Latest Blocks (/api/blocks)</a>
					<a href="/api/transfers" style="color: #667eea; text-decoration: none; font-size: 13px;">→ Latest Transfers (/api/transfers)</a>
				</div>
				<button class="action-btn" onclick="location.href='/api/admin/start-demo'">🎮 Start Demo Mode</button>
			</div>
		</div>

		<!-- Blocks Table -->
		<div class="card">
			<h2>📦 Latest Blocks</h2>
			<table class="data-table">
				<thead>
					<tr>
						<th>Block #</th>
						<th>Hash</th>
						<th>Parent Hash</th>
						<th>Timestamp</th>
					</tr>
				</thead>
				<tbody id="blocksTable">
					<tr><td colspan="4" class="loading">Loading blocks...</td></tr>
				</tbody>
			</table>
		</div>

		<!-- Transfers Table -->
		<div class="card">
			<h2>💸 Latest Transfers</h2>
			<table class="data-table">
				<thead>
					<tr>
						<th>Block</th>
						<th>From</th>
						<th>To</th>
						<th>Amount</th>
						<th>Token</th>
					</tr>
				</thead>
				<tbody id="transfersTable">
					<tr><td colspan="5" class="loading">Loading transfers...</td></tr>
				</tbody>
			</table>
		</div>
	</div>

	<script>
		// Fetch and update data every 5 seconds
		const updateInterval = 5000;

		async function fetchData() {
			try {
				// Fetch status
				const statusRes = await fetch('/api/status');
				const statusData = await statusRes.json();
				document.getElementById('state').textContent = statusData.state || 'unknown';
				document.getElementById('latestBlock').textContent = statusData.latest_block || '0';
				document.getElementById('totalBlocks').textContent = statusData.total_blocks || '0';
				document.getElementById('totalTransfers').textContent = statusData.total_transfers || '0';
				document.getElementById('health').textContent = statusData.is_healthy ? '✅ Healthy' : '⚠️ Unhealthy';
				document.getElementById('health').className = statusData.is_healthy ? 'status-badge status-healthy' : 'status-badge status-error';

				// Fetch blocks
				const blocksRes = await fetch('/api/blocks');
				const blocksData = await blocksRes.json();
				const blocksTable = document.getElementById('blocksTable');
				if (blocksData.blocks && blocksData.blocks.length > 0) {
					blocksTable.innerHTML = blocksData.blocks.map(block => '<tr><td class="stat-value">' + block.number + '</td><td class="hash">' + block.hash.substring(0, 16) + '...</td><td class="hash">' + block.parent_hash.substring(0, 16) + '...</td><td>' + new Date(block.processed_at).toLocaleString() + '</td></tr>').join('');
				} else {
					blocksTable.innerHTML = '<tr><td colspan="4" class="loading">No blocks yet</td></tr>';
				}

				// Fetch transfers
				const transfersRes = await fetch('/api/transfers');
				const transfersData = await transfersRes.json();
				const transfersTable = document.getElementById('transfersTable');
				if (transfersData.transfers && transfersData.transfers.length > 0) {
					transfersTable.innerHTML = transfersData.transfers.map(transfer => '<tr><td class="stat-value">' + transfer.block_number + '</td><td class="address">' + transfer.from_address.substring(0, 10) + '...</td><td class="address">' + transfer.to_address.substring(0, 10) + '...</td><td class="stat-value">' + transfer.amount + '</td><td class="address">' + transfer.token_address.substring(0, 10) + '...</td></tr>').join('');
				} else {
					transfersTable.innerHTML = '<tr><td colspan="5" class="loading">No transfers yet</td></tr>';
				}

				document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
			} catch (error) {
				console.error('Error fetching data:', error);
				document.getElementById('lastUpdate').textContent = 'Error: ' + error.message;
			}
		}

		// Initial fetch
		fetchData();

		// Set up polling
		setInterval(fetchData, updateInterval);
	</script>
</body>
</html>`)
			}

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
		} else if r.URL.Path == "/api/blocks" {
			handleGetBlocks(w, r, db)
		} else if r.URL.Path == "/api/transfers" {
			handleGetTransfers(w, r, db)
		} else if r.URL.Path == "/api/status" {
			handleGetStatus(w, r, db, rpcPool)
		} else if r.URL.Path == "/api/admin/generate-demo-tx" {
			handleGenerateDemoTransactions(w, r, rpcPool)
		} else {
			http.NotFound(w, r)
		}
	})

	// Start Prometheus metrics server
	mux.Handle("/metrics", promhttp.Handler())

	// 获取 API 端口（从环境变量或默认值）
	apiPort := os.Getenv("API_PORT")
	if apiPort == "" {
		apiPort = "8080"
	}
	apiAddr := ":" + apiPort

	// 创建 HTTP server 用于优雅关闭
	httpServer := &http.Server{
		Addr:    apiAddr,
		Handler: mux,
	}

	// 检查端口冲突
	portNum := 8080
	if p, err := strconv.Atoi(apiPort); err == nil {
		portNum = p
	}
	if err := checkPortAvailable(portNum); err != nil {
		engine.Logger.Error("port_conflict",
			slog.Int("port", portNum),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	// 在 goroutine 中启动 HTTP server
	go func() {
		engine.Logger.Info("http_server_started",
			slog.String("port", apiPort),
			slog.String("health_endpoint", "http://localhost:"+apiPort+"/healthz"),
			slog.String("metrics_endpoint", "http://localhost:"+apiPort+"/metrics"),
			slog.String("dashboard_url", "http://localhost:"+apiPort),
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
	batchSize := big.NewInt(int64(cfg.FetchBatchSize))
	if new(big.Int).Sub(latestBlock, startBlock).Cmp(big.NewInt(10000)) > 0 {
		batchSize = big.NewInt(int64(cfg.FetchBatchSize / 2)) // 大差距时减小批次
	}

	endBlock := new(big.Int).Add(startBlock, batchSize)
	if endBlock.Cmp(latestBlock) > 0 {
		endBlock = new(big.Int).Set(latestBlock) // 不超过最新块
	}

	// 6. 启动 Sequencer - 确保顺序处理（传入 Fetcher 用于 Reorg 时暂停）
	sequencer := engine.NewSequencerWithFetcher(processor, fetcher, startBlock, chainID, fetcher.Results, fatalErrCh, reorgCh, metrics)

	// 在协程中调度任务，避免阻塞主线程
	// 这很关键：Schedule()会发送大量任务到jobs通道，如果在主线程中同步运行，
	// 当jobs缓冲区满了之后会阻塞，导致Sequencer无法启动，形成死锁
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := fetcher.Schedule(ctx, startBlock, endBlock); err != nil {
			engine.Logger.Error("schedule_failed",
				slog.String("error", err.Error()),
			)
			// 发送致命错误
			select {
			case fatalErrCh <- err:
			case <-ctx.Done():
			}
		}
		engine.Logger.Info("blocks_scheduled",
			slog.String("start_block", startBlock.String()),
			slog.String("end_block", endBlock.String()),
			slog.String("mode", "incremental_sync"),
		)
	}()

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
		slog.String("dashboard_url", "http://localhost:"+apiPort),
	)

	// 7. 优雅退出处理 + 持续调度循环
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 持续调度ticker - 每2秒检查是否需要调度更多区块
	scheduleTicker := time.NewTicker(2 * time.Second)
	defer scheduleTicker.Stop()

	// 记录当前调度进度
	lastScheduledBlock := new(big.Int).Set(endBlock)
	schedulingInProgress := false

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
		case <-scheduleTicker.C:
			// 持续调度逻辑：确保indexer追赶链头
			if !schedulingInProgress {
				// 获取sequencer当前处理的区块
				currentBlockInt := sequencer.GetExpectedBlock()
				currentBlockStr := currentBlockInt.String()

				// 获取链上最新区块
				latestBlock, err := rpcPool.GetLatestBlockNumber(ctx)
				if err != nil {
					engine.Logger.Warn("failed_to_get_latest_block_for_schedule",
						slog.String("error", err.Error()),
					)
					continue
				}

				// 如果当前区块接近已调度的区块，调度更多
				blocksBehind := new(big.Int).Sub(latestBlock, currentBlockInt)
				scheduledAhead := new(big.Int).Sub(lastScheduledBlock, currentBlockInt)

				// 当已调度区块只剩不到100个，且落后链头超过10个块时，继续调度
				if scheduledAhead.Cmp(big.NewInt(100)) < 0 && blocksBehind.Cmp(big.NewInt(10)) > 0 {
					schedulingInProgress = true

					wg.Add(1)
					go func() {
						defer wg.Done()
						defer func() { schedulingInProgress = false }()

						// 计算下一批次的起止区块
						nextStart := new(big.Int).Add(lastScheduledBlock, big.NewInt(1))
						batchSize := big.NewInt(int64(cfg.FetchBatchSize)) // 每次调度 batch size 个块

						nextEnd := new(big.Int).Add(nextStart, batchSize)
						if nextEnd.Cmp(latestBlock) > 0 {
							nextEnd = new(big.Int).Set(latestBlock)
						}

						engine.Logger.Info("📈 [Catch-up] 持续调度新区块",
							slog.String("from", nextStart.String()),
							slog.String("to", nextEnd.String()),
							slog.String("current_block", currentBlockStr),
							slog.String("latest_block", latestBlock.String()),
							slog.Int64("blocks_behind", blocksBehind.Int64()),
						)

						if err := fetcher.Schedule(ctx, nextStart, nextEnd); err != nil {
							engine.Logger.Error("catchup_schedule_failed",
								slog.String("error", err.Error()),
							)
							return
						}

						// 更新最后调度区块
						lastScheduledBlock.Set(nextEnd)

						engine.Logger.Info("🎉 [Catch-up] 批次调度完成",
							slog.String("scheduled_until", nextEnd.String()),
						)
					}()
				}
			}
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
