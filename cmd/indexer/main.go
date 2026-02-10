package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/emulator"
	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/web"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "github.com/jackc/pgx/v5/stdlib" // PGX Driver
	"github.com/jmoiron/sqlx"
)

var cfg *config.Config

// 全局自修复事件计数器
var selfHealingEvents atomic.Uint64

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
	w.fetcher.Start(ctx, w.wg)
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

func (w *IndexerServiceWrapper) SetLowPowerMode(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fetcher != nil {
		w.fetcher.SetHeaderOnlyMode(enabled)
	}
}

func (w *IndexerServiceWrapper) SetSequencer(sequencer *engine.Sequencer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sequencer = sequencer
}

func maskDatabaseURL(url string) string {
	if len(url) > 20 {
		return url[:10] + "***" + url[len(url)-10:]
	}
	return "***"
}

func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, rpcPool *engine.RPCClientPool, chainID int64) (*big.Int, error) {
	var lastSyncedBlock string
	err := db.GetContext(ctx, &lastSyncedBlock,
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)

	latestChainBlock, rpcErr := rpcPool.GetLatestBlockNumber(ctx)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			engine.Logger.Info("no_checkpoint_found", slog.Int64("chain_id", chainID), slog.String("action", "starting_from_0"))
			return big.NewInt(0), nil
		}
		return nil, err
	}

	blockNum, ok := new(big.Int).SetString(lastSyncedBlock, 10)
	if !ok {
		return nil, fmt.Errorf("invalid block number: %s", lastSyncedBlock)
	}

	if rpcErr == nil && blockNum.Cmp(latestChainBlock) > 0 {
		selfHealingEvents.Add(1)
		engine.Logger.Warn("🚨 CHECKPOINT_DRIFT_DETECTED",
			slog.String("db_checkpoint", blockNum.String()),
			slog.String("chain_tip", latestChainBlock.String()),
			slog.String("action", "auto_resetting_to_chain_tip"),
		)
		return latestChainBlock, nil
	}

	startBlock := new(big.Int).Add(blockNum, big.NewInt(1))
	engine.LogCheckpointResumed(blockNum.String(), startBlock.String())
	return startBlock, nil
}

// REST Models
type Block struct {
	Number      string    `db:"number" json:"number"`
	Hash        string    `db:"hash" json:"hash"`
	ParentHash  string    `db:"parent_hash" json:"parent_hash"`
	Timestamp   string    `db:"timestamp" json:"timestamp"`
	ProcessedAt time.Time `db:"processed_at" json:"processed_at"`
}

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

func handleGetBlocks(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	var blocks []Block
	err := db.SelectContext(r.Context(), &blocks, "SELECT number, hash, parent_hash, timestamp, processed_at FROM blocks ORDER BY number DESC LIMIT 10")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"blocks": blocks})
}

func handleGetTransfers(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	var transfers []Transfer
	err := db.SelectContext(r.Context(), &transfers, "SELECT id, block_number, tx_hash, log_index, from_address, to_address, amount, token_address FROM transfers ORDER BY block_number DESC LIMIT 10")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"transfers": transfers})
}

func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool *engine.RPCClientPool) {
	latestChainBlock, _ := rpcPool.GetLatestBlockNumber(r.Context())
	var latestIndexedBlock string
	_ = db.GetContext(r.Context(), &latestIndexedBlock, "SELECT COALESCE(MAX(number), '0') FROM blocks")
	
	lib, _ := strconv.ParseInt(latestIndexedBlock, 10, 64)
	syncLag := int64(0)
	if latestChainBlock != nil {
		syncLag = latestChainBlock.Int64() - lib
	}

	var totalBlocks, totalTransfers int64
	_ = db.GetContext(r.Context(), &totalBlocks, "SELECT COUNT(*) FROM blocks")
	_ = db.GetContext(r.Context(), &totalTransfers, "SELECT COUNT(*) FROM transfers")

	status := map[string]interface{}{
		"state":              "active",
		"latest_block":       latestChainBlock.String(), // 💡 统一 Key 名为 latest_block
		"latest_indexed":     latestIndexedBlock,
		"sync_lag":           syncLag,
		"total_blocks":       totalBlocks,
		"total_transfers":    totalTransfers,
		"is_healthy":         rpcPool.GetHealthyNodeCount() > 0,
		"self_healing_count": selfHealingEvents.Load(),
		"rpc_nodes": map[string]int{
			"healthy": rpcPool.GetHealthyNodeCount(),
			"total":   rpcPool.GetTotalNodeCount(),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func main() {
	cfg = config.Load()
	engine.InitLogger(cfg.LogLevel)

	db, err := sqlx.Connect("pgx", cfg.DatabaseURL)
	if err != nil { slog.Error("db_fail", "err", err); os.Exit(1) }
	defer db.Close()

	rpcPool, _ := engine.NewRPCClientPoolWithTimeout(cfg.RPCURLs, cfg.RPCTimeout)
	rpcPool.SetRateLimit(cfg.RPCRateLimit, cfg.RPCRateLimit*2)
	defer rpcPool.Close()

	fetcher := engine.NewFetcher(rpcPool, cfg.FetchConcurrency)
	processor := engine.NewProcessor(db, rpcPool, cfg.RetryQueueSize, cfg.ChainID)
	processor.SetBatchCheckpoint(cfg.CheckpointBatch)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	wsHub := web.NewHub()
	wg.Add(1)
	go func() { defer wg.Done(); wsHub.Run(ctx) }()

	processor.EventHook = func(eventType string, data interface{}) {
		wsHub.Broadcast(web.WSEvent{Type: eventType, Data: data})
	}

	// 仿真器逻辑
	emuConfig := emulator.LoadConfig()
	if emuConfig.Enabled {
		emu, _ := emulator.NewEmulator(emuConfig.RpcURL, emuConfig.PrivateKey)
		emu.OnSelfHealing = func(r string) { selfHealingEvents.Add(1) }
		emu.SetSecurityConfig(cfg.MaxGasPrice, cfg.GasSafetyMargin)
		wg.Add(1)
		go func() { defer wg.Done(); emu.Start(ctx, nil) }()
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/static/", web.HandleStatic())
	mux.HandleFunc("/", web.RenderDashboard)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { wsHub.HandleWS(w, r) })
	
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { handleGetStatus(w, r, db, rpcPool) })
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) { handleGetBlocks(w, r, db) })
	mux.HandleFunc("/api/transfers", func(w http.ResponseWriter, r *http.Request) { handleGetTransfers(w, r, db) })

	server := &http.Server{Addr: "0.0.0.0:8080", Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http_server_fail", "err", err)
		}
	}()

	// 启动抓取
	startBlock, _ := getStartBlockFromCheckpoint(ctx, db, rpcPool, cfg.ChainID)
	fetcher.Start(ctx, &wg)
	
	// 启动数据审计器 (Reconciler)
	reconciler := engine.NewReconciler(db, rpcPool, engine.GetMetrics())
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 每 10 分钟审计一次，回溯 500 个区块
		reconciler.StartPeriodicAudit(ctx, 10*time.Minute, 500)
	}()

	sequencer := engine.NewSequencerWithFetcher(processor, fetcher, startBlock, cfg.ChainID, fetcher.Results, nil, nil, engine.GetMetrics())
	wg.Add(1)
	go func() { defer wg.Done(); sequencer.Run(ctx) }()

	go fetcher.Schedule(ctx, startBlock, new(big.Int).Add(startBlock, big.NewInt(1000)))

	// 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	
	cancel()
	server.Shutdown(context.Background())
	wg.Wait()
}