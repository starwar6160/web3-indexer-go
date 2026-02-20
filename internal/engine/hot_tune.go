package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// 🔥 HotTuneManager 热调优管理器
// 允许在不重启进程的情况下，动态调整系统性能参数
// 用于演示时切换节能/全速模式，或生产环境应急调优

type HotTuneManager struct {
	mu sync.RWMutex

	// 可调优组件引用
	rpcPool RPCClient

	// 当前配置
	rpcLimit  rate.Limit
	rpcBurst  int
	tunedAt   time.Time
	tuneCount int
}

// HotTuneConfig 热调优配置参数
type HotTuneConfig struct {
	RPCQPS   *float64 `json:"rpc_qps,omitempty"`   // RPC 每秒请求数
	RPCBurst *int     `json:"rpc_burst,omitempty"` // RPC 突发容量
}

// HotTuneResult 调优结果
type HotTuneResult struct {
	Success       bool      `json:"success"`
	Message       string    `json:"message"`
	AppliedAt     time.Time `json:"applied_at"`
	PreviousQPS   float64   `json:"previous_qps"`
	CurrentQPS    float64   `json:"current_qps"`
	PreviousBurst int       `json:"previous_burst"`
	CurrentBurst  int       `json:"current_burst"`
	TuneCount     int       `json:"tune_count"`
}

var globalHotTuneManager *HotTuneManager

// InitHotTuneManager 初始化热调优管理器
func InitHotTuneManager(rpcPool RPCClient) *HotTuneManager {
	globalHotTuneManager = &HotTuneManager{
		rpcPool:  rpcPool,
		rpcLimit: rate.Limit(1000), // 默认 1000 QPS
		rpcBurst: 100,
		tunedAt:  time.Now(),
	}
	slog.Info("🔥 HotTuneManager initialized", "default_qps", 1000)
	return globalHotTuneManager
}

// GetHotTuneManager 获取全局热调优管理器
func GetHotTuneManager() *HotTuneManager {
	return globalHotTuneManager
}

// ApplyHotTune 应用热调优配置
func (h *HotTuneManager) ApplyHotTune(ctx context.Context, config HotTuneConfig) HotTuneResult {
	h.mu.Lock()
	defer h.mu.Unlock()

	result := HotTuneResult{
		AppliedAt:     time.Now(),
		PreviousQPS:   float64(h.rpcLimit),
		PreviousBurst: h.rpcBurst,
		TuneCount:     h.tuneCount + 1,
	}

	// 验证并应用 RPC QPS 配置
	if config.RPCQPS != nil {
		newLimit := rate.Limit(*config.RPCQPS)
		if newLimit < 0 || newLimit > 10000 {
			result.Success = false
			result.Message = fmt.Sprintf("RPC QPS %v out of allowed range [0, 10000]", *config.RPCQPS)
			return result
		}
		h.rpcLimit = newLimit
	}

	// 验证并应用 Burst 配置
	if config.RPCBurst != nil {
		if *config.RPCBurst < 1 || *config.RPCBurst > 1000 {
			result.Success = false
			result.Message = fmt.Sprintf("RPC Burst %d out of allowed range [1, 1000]", *config.RPCBurst)
			return result
		}
		h.rpcBurst = *config.RPCBurst
	}

	// 应用配置到 RPC Pool
	if h.rpcPool != nil {
		if enhancedPool, ok := h.rpcPool.(*EnhancedRPCClientPool); ok {
			enhancedPool.SetRateLimit(float64(h.rpcLimit), h.rpcBurst)
			slog.Info("🔥 HotTune: RPC rate limit updated",
				"new_qps", h.rpcLimit,
				"new_burst", h.rpcBurst)
		}
	}

	h.tunedAt = time.Now()
	h.tuneCount++

	result.Success = true
	result.Message = "Hot tune applied successfully"
	result.CurrentQPS = float64(h.rpcLimit)
	result.CurrentBurst = h.rpcBurst

	return result
}

// GetCurrentConfig 获取当前配置（用于 /debug/hotune/status）
func (h *HotTuneManager) GetCurrentConfig() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"rpc_qps":                 float64(h.rpcLimit),
		"rpc_burst":               h.rpcBurst,
		"tuned_at":                h.tunedAt.Format(time.RFC3339),
		"tune_count":              h.tuneCount,
		"seconds_since_last_tune": time.Since(h.tunedAt).Seconds(),
	}
}

// PresetMode 预定义调优模式
func (h *HotTuneManager) PresetMode(mode string) HotTuneResult {
	switch mode {
	case "ECO":
		// 节能模式：极低消耗
		qps := 2.0
		burst := 1
		return h.ApplyHotTune(context.Background(), HotTuneConfig{
			RPCQPS:   &qps,
			RPCBurst: &burst,
		})
	case "BALANCED":
		// 平衡模式：适度性能
		qps := 100.0
		burst := 20
		return h.ApplyHotTune(context.Background(), HotTuneConfig{
			RPCQPS:   &qps,
			RPCBurst: &burst,
		})
	case "BEAST":
		// 野兽模式：全速压榨
		qps := 5000.0
		burst := 500
		return h.ApplyHotTune(context.Background(), HotTuneConfig{
			RPCQPS:   &qps,
			RPCBurst: &burst,
		})
	case "YOLO":
		// YOLO模式：无限制（谨慎使用）
		qps := 10000.0
		burst := 1000
		return h.ApplyHotTune(context.Background(), HotTuneConfig{
			RPCQPS:   &qps,
			RPCBurst: &burst,
		})
	default:
		return HotTuneResult{
			Success: false,
			Message: fmt.Sprintf("Unknown preset mode: %s. Available: ECO, BALANCED, BEAST, YOLO", mode),
		}
	}
}

// --- HTTP Handler 集成 ---

// RegisterHotTuneRoutes 注册热调优端点
func RegisterHotTuneRoutes(mux *http.ServeMux) {
	// GET /debug/hotune/status - 查看当前配置
	mux.HandleFunc("/debug/hotune/status", handleHotTuneStatus)

	// POST /debug/hotune/apply - 应用自定义配置
	mux.HandleFunc("/debug/hotune/apply", handleHotTuneApply)

	// POST /debug/hotune/preset - 应用预设模式
	mux.HandleFunc("/debug/hotune/preset", handleHotTunePreset)
}

func handleHotTuneStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manager := GetHotTuneManager()
	if manager == nil {
		http.Error(w, "HotTuneManager not initialized", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, manager.GetCurrentConfig())
}

func handleHotTuneApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manager := GetHotTuneManager()
	if manager == nil {
		http.Error(w, "HotTuneManager not initialized", http.StatusServiceUnavailable)
		return
	}

	var config HotTuneConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	result := manager.ApplyHotTune(r.Context(), config)
	writeJSON(w, result)
}

func handleHotTunePreset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manager := GetHotTuneManager()
	if manager == nil {
		http.Error(w, "HotTuneManager not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	result := manager.PresetMode(req.Mode)
	writeJSON(w, result)
}
