package engine

import (
	"encoding/json"
	"net/http"
	"time"
)

// AdminServer 管理员API服务器
type AdminServer struct {
	stateManager *StateManager
}

// NewAdminServer 创建管理员服务器
func NewAdminServer(stateManager *StateManager) *AdminServer {
	return &AdminServer{
		stateManager: stateManager,
	}
}

// RegisterRoutes 注册管理员路由
func (a *AdminServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/start-demo", a.StartDemo)
	mux.HandleFunc("/api/admin/stop", a.Stop)
	mux.HandleFunc("/api/admin/status", a.GetStatus)
	mux.HandleFunc("/api/admin/config", a.GetConfig)
}

// StartDemo 启动演示模式
func (a *AdminServer) StartDemo(w http.ResponseWriter, r *http.Request) {
	// 只允许POST请求
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 记录访问
	a.stateManager.RecordAccess()

	// 手动启动演示模式
	a.stateManager.StartDemo()

	response := map[string]interface{}{
		"success":   true,
		"message":   "Demo mode started",
		"state":     a.stateManager.GetState().String(),
		"duration":  "5m0s", // 硬编码5分钟
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	Logger.Info("demo_started_via_api",
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
	)
}

// Stop 停止索引器
func (a *AdminServer) Stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 强制转换到闲置状态
	a.stateManager.transitionTo(StateIdle)

	response := map[string]interface{}{
		"success":   true,
		"message":   "Indexer stopped",
		"state":     a.stateManager.GetState().String(),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	Logger.Info("indexer_stopped_via_api",
		"remote_addr", r.RemoteAddr,
	)
}

// GetStatus 获取系统状态
func (a *AdminServer) GetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 记录访问
	a.stateManager.RecordAccess()

	status := a.stateManager.GetStatus()

	// 添加RPC节点状态
	healthyNodes := a.stateManager.rpcPool.GetHealthyNodeCount()
	status["rpc_nodes"] = map[string]interface{}{
		"healthy": healthyNodes,
		"total":   healthyNodes, // 暂时使用健康节点数，后续可以通过RPC池获取总数
	}

	// 添加成本估算
	status["cost_optimization"] = map[string]interface{}{
		"current_mode":         a.stateManager.GetState().String(),
		"rpc_quota_usage":      a.getRPCQuotaUsage(a.stateManager.GetState()),
		"estimated_daily_cost": a.getEstimatedDailyCost(a.stateManager.GetState()),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// GetConfig 获取配置信息
func (a *AdminServer) GetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 记录访问
	a.stateManager.RecordAccess()

	config := map[string]interface{}{
		"demo_duration":    a.stateManager.demoDuration.String(),
		"idle_timeout":     a.stateManager.idleTimeout.String(),
		"check_interval":   a.stateManager.checkInterval.String(),
		"supported_states": []string{"idle", "active", "watching"},
		"features": map[string]interface{}{
			"auto_sleep":        true,
			"cost_optimization": true,
			"demo_mode":         true,
			"wss_keepalive":     false, // TODO: 实现后改为true
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(config)
}

// getRPCQuotaUsage 获取RPC配额使用估算
func (a *AdminServer) getRPCQuotaUsage(state IndexerState) string {
	switch state {
	case StateActive:
		return "HIGH (eth_getLogs intensive)"
	case StateIdle:
		return "NONE (no RPC calls)"
	case StateWatching:
		return "MINIMAL (WSS subscription only)"
	default:
		return "UNKNOWN"
	}
}

// getEstimatedDailyCost 获取预估日成本（基于Infura免费额度）
func (a *AdminServer) getEstimatedDailyCost(state IndexerState) string {
	switch state {
	case StateActive:
		return "~2.4M credits/day (80% of free tier)"
	case StateIdle:
		return "~0 credits/day (0% of free tier)"
	case StateWatching:
		return "~0.1M credits/day (3% of free tier)"
	default:
		return "UNKNOWN"
	}
}

// Middleware 记录所有API访问
func (a *AdminServer) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 记录访问时间
		a.stateManager.RecordAccess()

		// 记录请求日志
		Logger.Info("admin_api_access",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		// 执行下一个处理器
		next(w, r)
	}
}
