package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// HealthStatus 健康状态响应
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]Check  `json:"checks"`
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
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// 1. 获取链上最新块
	latestChainBlock, _ := h.rpcPool.GetLatestBlockNumber(ctx)

	// 2. 获取索引器进度
	var expectedBlock string
	bufferSize := 0
	if h.sequencer != nil {
		expectedBlock = h.sequencer.GetExpectedBlock().String()
		bufferSize = h.sequencer.GetBufferSize()
	}

	// 3. 计算延迟 (Sync Lag)
	var syncLag int64 = 0
	if latestChainBlock != nil && h.sequencer != nil {
		syncLag = latestChainBlock.Int64() - h.sequencer.GetExpectedBlock().Int64()
	}

	status := map[string]interface{}{
		"is_healthy":         h.rpcPool.GetHealthyNodeCount() > 0,
		"latest_chain_block": latestChainBlock.String(),
		"indexed_block":      expectedBlock,
		"sync_lag":           syncLag,
		"buffer_size":        bufferSize,
		"rpc_nodes": map[string]int{
			"healthy": h.rpcPool.GetHealthyNodeCount(),
			"total":   h.rpcPool.GetTotalNodeCount(),
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
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
	if dbCheck.Status != "healthy" {
		allHealthy = false
	}

	// 2. RPC 连接检查
	rpcCheck := h.checkRPC(ctx)
	status.Checks["rpc"] = rpcCheck
	if rpcCheck.Status != "healthy" {
		allHealthy = false
	}

	// 3. Sequencer 状态检查
	sequencerCheck := h.checkSequencer(ctx)
	status.Checks["sequencer"] = sequencerCheck
	if sequencerCheck.Status != "healthy" {
		allHealthy = false
	}

	// 4. Fetcher 状态检查
	fetcherCheck := h.checkFetcher(ctx)
	status.Checks["fetcher"] = fetcherCheck
	if fetcherCheck.Status != "healthy" {
		allHealthy = false
	}

	if allHealthy {
		status.Status = "healthy"
		w.WriteHeader(http.StatusOK)
	} else {
		status.Status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(status)
}