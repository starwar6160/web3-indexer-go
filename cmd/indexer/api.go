package main

import (
	"encoding/json"
	"net/http"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/jmoiron/sqlx"
)

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

	var totalBlocks, totalTransfers int64
	_ = db.GetContext(r.Context(), &totalBlocks, "SELECT COUNT(*) FROM blocks")
	_ = db.GetContext(r.Context(), &totalTransfers, "SELECT COUNT(*) FROM transfers")

	latestBlockStr := "0"
	var syncLag int64
	if latestChainBlock != nil {
		latestBlockStr = latestChainBlock.String()
		syncLag = latestChainBlock.Int64() - totalBlocks
		if syncLag < 0 {
			syncLag = 0
		}
	}

	status := map[string]interface{}{
		"state":              "active",
		"latest_block":       latestBlockStr,
		"latest_indexed":     latestIndexedBlock,
		"sync_lag":           syncLag,
		"total_blocks":       totalBlocks,
		"total_transfers":    totalTransfers,
		"tps":                currentTPS.Load(),
		"bps":                currentBPS.Load(),
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
