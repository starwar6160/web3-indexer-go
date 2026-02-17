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
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// 1. 获取链上最新块
	latestChainBlock, err := h.rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		Logger.Error("failed_to_get_latest_block_in_health_status", "err", err)
	}

	// 2. 获取索引器进度
	var expectedBlock string
	bufferSize := 0
	if h.sequencer != nil {
		expectedBlock = h.sequencer.GetExpectedBlock().String()
		bufferSize = h.sequencer.GetBufferSize()
	}

	// 3. 计算延迟 (Sync Lag)
	var syncLag int64
	var timeTravel bool
	if latestChainBlock != nil && h.sequencer != nil {
		dbHeight := h.sequencer.GetExpectedBlock().Int64()
		rpcHeight := latestChainBlock.Int64()
		syncLag = rpcHeight - dbHeight

		// 🚨 穿越判定：如果数据库跑到了链的前面
		if dbHeight > rpcHeight {
			timeTravel = true
			Logger.Warn("🚨 CRITICAL: Time-travel detected! DB is ahead of Chain.",
				"db_height", dbHeight,
				"rpc_height", rpcHeight,
				"diff", dbHeight-rpcHeight)
		}
	}

	latestBlockStr := "0"
	if latestChainBlock != nil {
		latestBlockStr = latestChainBlock.String()
	}

	status := map[string]interface{}{
		"is_healthy":         h.rpcPool.GetHealthyNodeCount() > 0,
		"latest_chain_block": latestBlockStr,
		"indexed_block":      expectedBlock,
		"sync_lag":           syncLag,
		"time_travel":        timeTravel, // 🚀 暴露给 UI 的穿越标志
		"buffer_size":        bufferSize,
		"rpc_nodes": map[string]int{
			"healthy": h.rpcPool.GetHealthyNodeCount(),
			"total":   h.rpcPool.GetTotalNodeCount(),
		},
		"timestamp": time.Now().Format(time.RFC3339),
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
