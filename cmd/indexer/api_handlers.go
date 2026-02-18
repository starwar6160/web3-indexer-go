package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/jmoiron/sqlx"
)

// REST Models
type Block struct {
	ProcessedAt string `db:"processed_at" json:"processed_at"`
	Number      string `db:"number" json:"number"`
	Hash        string `db:"hash" json:"hash"`
	ParentHash  string `db:"parent_hash" json:"parent_hash"`
	Timestamp   string `db:"timestamp" json:"timestamp"`
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
	Symbol       string `db:"symbol" json:"symbol"`
	Type         string `db:"activity_type" json:"type"`
}

func handleGetBlocks(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	type dbBlock struct {
		ProcessedAt time.Time `db:"processed_at"`
		Number      string    `db:"number"`
		Hash        string    `db:"hash"`
		ParentHash  string    `db:"parent_hash"`
		Timestamp   string    `db:"timestamp"`
	}
	var rawBlocks []dbBlock
	err := db.SelectContext(r.Context(), &rawBlocks, `SELECT number, hash, parent_hash, timestamp, processed_at FROM blocks ORDER BY number DESC LIMIT 10`)
	if err != nil {
		http.Error(w, "Failed to retrieve blocks", 500)
		return
	}

	blocks := make([]Block, len(rawBlocks))
	for i, b := range rawBlocks {
		blocks[i] = Block{
			Number:      b.Number,
			Hash:        b.Hash,
			ParentHash:  b.ParentHash,
			Timestamp:   b.Timestamp,
			ProcessedAt: b.ProcessedAt.Format("15:04:05.000"),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"blocks": blocks}); err != nil {
		slog.Error("failed_to_encode_blocks", "err", err)
	}
}

func handleGetTransfers(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	var transfers []Transfer
	err := db.SelectContext(r.Context(), &transfers, "SELECT id, block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type FROM transfers ORDER BY block_number DESC, log_index DESC LIMIT 10")
	if err != nil {
		http.Error(w, "Failed to retrieve transfers", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"transfers": transfers}); err != nil {
		slog.Error("failed_to_encode_transfers", "err", err)
	}
}

func handleGetTransfersFromHotBuffer(w http.ResponseWriter, processor *engine.Processor) {
	hotTransfers := processor.GetHotBuffer().GetLatest(10)
	apiTransfers := make([]Transfer, len(hotTransfers))
	for i, t := range hotTransfers {
		// #nosec G115 - LogIndex is within safe range for int
		apiTransfers[i] = Transfer{
			BlockNumber:  t.BlockNumber.String(),
			TxHash:       t.TxHash,
			LogIndex:     int(t.LogIndex),
			FromAddress:  t.From,
			ToAddress:    t.To,
			Amount:       t.Amount.String(),
			TokenAddress: t.TokenAddress,
			Symbol:       t.Symbol,
			Type:         t.Type,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"transfers": apiTransfers,
		"source":    "hot_buffer",
	}); err != nil {
		slog.Error("failed_to_encode_hot_transfers", "err", err)
	}
}

func handleInitialStatus(w http.ResponseWriter, title string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"state": "initializing",
		"title": title,
		"msg":   "Database or RPC not ready yet",
	}); err != nil {
		slog.Error("failed_to_encode_init_status", "err", err)
	}
}

func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool engine.RPCClient, lazyManager *engine.LazyManager, chainID int64, signer *engine.SignerMachine) {
	if lazyManager != nil {
		lazyManager.Trigger()
	}

	ctx := r.Context()

	// 🔥 原子状态更新：Latest (on Chain) = max(Fetcher_Current, RPC_Latest)
	// 解决指标更新滞后问题：Dashboard 显示的高度可能落后于 TailFollow 实际调度的高度
	//
	// 策略：
	// 1. 优先使用 HeightOracle（TailFollow 推送）
	// 2. 如果为 0（冷启动），则从 RPC 拉取
	// 3. Anvil 环境：每次强制刷新（消除缓存）
	snap := engine.GetHeightOracle().Snapshot()
	latestChainInt64 := snap.ChainHead

	// 冷启动或 Anvil 环境：强制从 RPC 获取最新高度
	if latestChainInt64 == 0 || chainID == 31337 {
		if tip, err := rpcPool.GetLatestBlockNumber(ctx); err == nil && tip != nil {
			rpcHeight := tip.Int64()
			// 只更新 RPC 高度更高时（避免回退）
			if rpcHeight > latestChainInt64 {
				engine.GetHeightOracle().SetChainHead(rpcHeight)
				latestChainInt64 = rpcHeight
			}
		}
	}

	// Use HeightOracle snapshot as the single source of truth for all height
	// numbers. This eliminates the race where /api/status calls GetLatestBlockNumber
	// on a potentially lagging RPC node while TailFollow has already advanced
	// further, producing "Synced > On-Chain" phantom readings.
	//
	// HeightOracle.ChainHead() is written exclusively by TailFollow (every 500ms),
	// which is the most authoritative and up-to-date source.
	// HeightOracle.IndexedHead() is written by Processor after each checkpoint commit.
	// Re-read snapshot after potential update
	snap = engine.GetHeightOracle().Snapshot()
	latestChainInt64 = snap.ChainHead
	latestIndexedBlockInt64 := snap.IndexedHead
	latestIndexedBlock := fmt.Sprintf("%d", latestIndexedBlockInt64)

	// If HeightOracle hasn't been populated yet (cold start before first TailFollow
	// tick), fall back to a live RPC call exactly once.
	if latestChainInt64 == 0 {
		if tip, err := rpcPool.GetLatestBlockNumber(ctx); err == nil && tip != nil {
			latestChainInt64 = tip.Int64()
			engine.GetHeightOracle().SetChainHead(latestChainInt64)
		}
	}
	if latestIndexedBlockInt64 == 0 {
		latestIndexedBlock = getLatestIndexedBlock(ctx, db)
		latestIndexedBlockInt64 = parseBlockNumber(latestIndexedBlock)
	}

	// 🔥 原子 Lag 计算：避免使用过时的快照值
	// 公式：SyncLag = max(0, LatestChain - LatestIndexed)
	// 不使用任何中间变量，直接从数据库实时查询
	var totalBlocksInDB int64
	err := db.GetContext(ctx, &totalBlocksInDB, "SELECT COUNT(*) FROM blocks")
	if err != nil {
		totalBlocksInDB = 0
	}

	// 实时计算 SyncLag（原子操作）
	rawSyncLag := latestChainInt64 - latestIndexedBlockInt64
	var syncLag int64
	var driftBlocks int64
	var isTimeTravel bool

	if rawSyncLag < 0 {
		// 时间旅行：Indexed > Chain（可能因为 RPC 节点滞后）
		driftBlocks = -rawSyncLag
		syncLag = 0
		isTimeTravel = driftBlocks > engine.GetHeightOracle().DriftTolerance
	} else {
		// 正常情况
		syncLag = rawSyncLag
		driftBlocks = 0
		isTimeTravel = false
	}

	e2eLatencyDisplay, e2eLatencySeconds := calculateLatency(ctx, db, latestChainInt64, latestIndexedBlockInt64, latestIndexedBlock)

	// 🚀 Local/Anvil/Replay Smoothing: Ignore astronomical latency
	if chainID == 31337 && e2eLatencySeconds > 3600 {
		e2eLatencyDisplay = "0.00s (Replay)"
		e2eLatencySeconds = 0
	}

	status := make(map[string]interface{})
	status["version"] = Version
	status["state"] = engine.ModeActive
	status["latest_block"] = fmt.Sprintf("%d", latestChainInt64)
	status["latest_indexed"] = latestIndexedBlock
	status["sync_lag"] = syncLag
	// time_travel: indexer has processed blocks beyond the reported chain head.
	// Caused by RPC node lag or stale HeightOracle (not a data integrity issue
	// unless drift_blocks > drift_tolerance).
	status["time_travel"] = isTimeTravel
	status["drift_blocks"] = driftBlocks
	// 🔥 实时查询总块数（避免使用过时的缓存值）
	status["total_blocks"] = totalBlocksInDB
	status["total_transfers"] = getCount(ctx, db, "SELECT COUNT(*) FROM transfers")
	status["tps"] = calculateTPS(ctx, db)
	status["is_catching_up"] = syncLag > 10
	status["is_healthy"] = rpcPool.GetHealthyNodeCount() > 0
	status["rpc_nodes"] = map[string]int{
		"healthy": rpcPool.GetHealthyNodeCount(),
		"total":   rpcPool.GetTotalNodeCount(),
	}
	status["e2e_latency_seconds"] = e2eLatencySeconds
	status["e2e_latency_display"] = e2eLatencyDisplay

	// 🎯 同步进度百分比计算（原子操作）
	// 规则：分母必须 >= 分子，否则百分比无意义。
	// 当 indexedHead > chainHead（时间旅行）时，进度视为 100%（已追上）。
	// 当 chainHead == 0（冷启动）时，进度为 0%。
	syncProgressPercent := 0.0
	if latestChainInt64 > 0 && latestIndexedBlockInt64 > 0 {
		if latestIndexedBlockInt64 >= latestChainInt64 {
			syncProgressPercent = 100.0
		} else {
			syncProgressPercent = float64(latestIndexedBlockInt64) / float64(latestChainInt64) * 100.0
		}
	}
	status["sync_progress_percent"] = syncProgressPercent

	// f5: Standby 模式下标记数据为 stale，让前端区分缓存数据和实时数据。
	// oracle_age_ms > 5000 表示 TailFollow 已超过 5 秒未更新链高度。
	oracleAgeMs := time.Since(snap.UpdatedAt).Milliseconds()
	if snap.UpdatedAt.IsZero() {
		oracleAgeMs = -1 // 尚未初始化
	}
	status["height_oracle_age_ms"] = oracleAgeMs
	status["data_is_stale"] = oracleAgeMs < 0 || oracleAgeMs > 5000

	if lazyManager != nil {
		lazyStatus := lazyManager.GetStatus()
		if mode, ok := lazyStatus["mode"].(string); ok {
			status["state"] = mode
			status["lazy_indexer"] = lazyStatus
			// Standby 模式下数据必然是 stale 的
			if mode == engine.ModeSleep {
				status["data_is_stale"] = true
			}
		}
	}

	if signer != nil {
		if signed, err := signer.Sign("status", status); err == nil {
			w.Header().Set("X-Payload-Signature", signed.Signature)
			w.Header().Set("X-Signer-ID", signed.SignerID)
			w.Header().Set("X-Public-Key", signed.PubKey)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Error("failed_to_encode_status", "err", err)
	}
}

func getLatestIndexedBlock(ctx context.Context, db *sqlx.DB) string {
	var latest string
	if err := db.GetContext(ctx, &latest, "SELECT COALESCE(MAX(number), '0') FROM blocks"); err != nil {
		return "0"
	}
	return latest
}

func getCount(ctx context.Context, db *sqlx.DB, query string) int64 {
	var count int64
	if err := db.GetContext(ctx, &count, query); err != nil {
		return 0
	}
	return count
}

func parseBlockNumber(s string) int64 {
	if s == "" || s == "0" {
		return 0
	}
	if parsed, ok := new(big.Int).SetString(s, 10); ok {
		return parsed.Int64()
	}
	return 0
}

func calculateLatency(ctx context.Context, db *sqlx.DB, latestChain, latestIndexed int64, latestIndexedStr string) (string, float64) {
	if latestChain <= 0 || latestIndexed <= 0 {
		return "0s", 0
	}
	syncLag := latestChain - latestIndexed
	if syncLag > 100 {
		estLatency := float64(syncLag) * 12
		return fmt.Sprintf("Catching up... (%d blocks behind)", syncLag), estLatency
	}
	var processedAt time.Time
	err := db.GetContext(ctx, &processedAt, "SELECT processed_at FROM blocks WHERE number = $1", latestIndexedStr)
	if err == nil && !processedAt.IsZero() {
		latency := time.Since(processedAt).Seconds()
		return fmt.Sprintf("%.2fs", latency), latency
	}
	return fmt.Sprintf("%.2fs", float64(syncLag)*12), float64(syncLag) * 12
}

func calculateTPS(_ context.Context, _ *sqlx.DB) float64 {
	return engine.GetMetrics().GetWindowTPS()
}
