package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// HealthStatus 健康状态响应
type HealthStatus struct {
	Timestamp time.Time        `json:"timestamp"`
	Status    string           `json:"status"`
	Checks    map[string]Check `json:"checks"`
}

// Check 单个检查项
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// HealthServer 健康检查服务器
type HealthServer struct {
	db        *sqlx.DB
	rpcPool   *RPCClientPool
	sequencer *Sequencer
	fetcher   *Fetcher
}

func NewHealthServer(db *sqlx.DB, rpcPool *RPCClientPool, sequencer *Sequencer, fetcher *Fetcher) *HealthServer {
	return &HealthServer{
		db:        db,
		rpcPool:   rpcPool,
		sequencer: sequencer,
		fetcher:   fetcher,
	}
}

// RegisterRoutes 注册健康检查路由
func (h *HealthServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.Healthz)
	mux.HandleFunc("/healthz/ready", h.Ready)
	mux.HandleFunc("/healthz/live", h.Live)
	mux.HandleFunc("/api/status", h.Status) // 详细的状态 API
}

// Status 返回索引器的实时运行状态
func (h *HealthServer) Status(w http.ResponseWriter, r *http.Request) {
	_, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// 🔥 横滨实验室：使用 HeightOracle 快照，确保一致性
	snap := GetHeightOracle().Snapshot()

	// 获取索引器进度
	bufferSize := 0
	if h.sequencer != nil {
		bufferSize = h.sequencer.GetBufferSize()
	}

	// 🔥 从快照读取数据，避免竞态条件
	syncLag := snap.SyncLag
	driftBlocks := snap.DriftBlocks
	isTimeTravel := snap.IsTimeTravel
	chainHead := snap.ChainHead
	indexedHead := snap.IndexedHead

	latestBlockStr := "0"
	if chainHead > 0 {
		latestBlockStr = fmt.Sprintf("%d", chainHead)
	}

	status := map[string]interface{}{
		"is_healthy":         h.rpcPool.GetHealthyNodeCount() > 0,
		"latest_chain_block": latestBlockStr,
		"indexed_block":      fmt.Sprintf("%d", indexedHead),
		"sync_lag":           syncLag,
		"drift_blocks":       driftBlocks,
		"time_travel":        isTimeTravel,
		"buffer_size":        bufferSize,
		"rpc_nodes": map[string]int{
			"healthy": h.rpcPool.GetHealthyNodeCount(),
			"total":   h.rpcPool.GetTotalNodeCount(),
		},
		"timestamp":            time.Now().Format(time.RFC3339),
		"height_oracle_age_ms": snap.UpdatedAt.UnixMilli(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(status); err != nil {
		Logger.Error("failed_to_encode_health_status", "err", err)
	}
}

// Healthz 完整健康检查
func (h *HealthServer) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status := HealthStatus{
		Timestamp: time.Now(),
		Checks:    make(map[string]Check),
	}

	allHealthy := true

	// 1. 数据库连接检查
	dbCheck := h.checkDatabase(ctx)
	status.Checks["database"] = dbCheck
	if dbCheck.Status != healthyStatus {
		allHealthy = false
	}

	// 2. RPC 连接检查
	rpcCheck := h.checkRPC(ctx)
	status.Checks["rpc"] = rpcCheck
	if rpcCheck.Status != healthyStatus {
		allHealthy = false
	}

	// 3. Sequencer 状态检查
	sequencerCheck := h.checkSequencer(ctx)
	status.Checks["sequencer"] = sequencerCheck
	if sequencerCheck.Status != healthyStatus {
		allHealthy = false
	}

	// 4. Fetcher 状态检查
	fetcherCheck := h.checkFetcher(ctx)
	status.Checks["fetcher"] = fetcherCheck
	if fetcherCheck.Status != healthyStatus {
		allHealthy = false
	}

	if allHealthy {
		status.Status = healthyStatus
		w.WriteHeader(http.StatusOK)
	} else {
		status.Status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		Logger.Error("failed_to_encode_healthz_response", "err", err)
	}
}
