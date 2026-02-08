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

// Ready 就绪检查（所有组件都准备好）
func (h *HealthServer) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// 快速检查关键组件
	dbCheck := h.checkDatabase(ctx)
	rpcCheck := h.checkRPC(ctx)

	if dbCheck.Status == "healthy" && rpcCheck.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"checks": map[string]Check{
				"database": dbCheck,
				"rpc":      rpcCheck,
			},
		})
	}
}

// Live 存活检查（进程是否存活）
func (h *HealthServer) Live(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "alive",
	})
}

// checkDatabase 检查数据库连接
func (h *HealthServer) checkDatabase(ctx context.Context) Check {
	start := time.Now()
	
	err := h.db.PingContext(ctx)
	latency := time.Since(start)
	
	if err != nil {
		return Check{
			Status:  "unhealthy",
			Message: err.Error(),
			Latency: latency.String(),
		}
	}
	
	return Check{
		Status:  "healthy",
		Latency: latency.String(),
	}
}

// checkRPC 检查 RPC 连接
func (h *HealthServer) checkRPC(ctx context.Context) Check {
	start := time.Now()
	
	// 获取最新区块号
	header, err := h.rpcPool.HeaderByNumber(ctx, nil)
	latency := time.Since(start)
	
	if err != nil {
		return Check{
			Status:  "unhealthy",
			Message: err.Error(),
			Latency: latency.String(),
		}
	}
	
	return Check{
		Status:  "healthy",
		Message: "latest_block: " + header.Number.String(),
		Latency: latency.String(),
	}
}

// checkSequencer 检查 Sequencer 状态
func (h *HealthServer) checkSequencer(ctx context.Context) Check {
	if h.sequencer == nil {
		return Check{
			Status:  "unhealthy",
			Message: "sequencer not initialized",
		}
	}
	
	bufferSize := h.sequencer.GetBufferSize()
	expectedBlock := h.sequencer.GetExpectedBlock()
	
	// 如果 buffer 过大，可能有问题
	if bufferSize > 500 {
		return Check{
			Status:  "degraded",
			Message: fmt.Sprintf("buffer_size: %d (high)", bufferSize),
		}
	}
	
	return Check{
		Status:  "healthy",
		Message: fmt.Sprintf("expected_block: %s, buffer_size: %d", expectedBlock.String(), bufferSize),
	}
}

// checkFetcher 检查 Fetcher 状态
func (h *HealthServer) checkFetcher(ctx context.Context) Check {
	if h.fetcher == nil {
		return Check{
			Status:  "unhealthy",
			Message: "fetcher not initialized",
		}
	}
	
	// 检查是否暂停
	if h.fetcher.IsPaused() {
		return Check{
			Status:  "degraded",
			Message: "fetcher paused (likely reorg handling)",
		}
	}
	
	return Check{
		Status:  "healthy",
		Message: "fetcher running",
	}
}

// SetSequencer 动态设置 sequencer（用于 main 初始化后注入）
func (h *HealthServer) SetSequencer(sequencer *Sequencer) {
	h.sequencer = sequencer
}
