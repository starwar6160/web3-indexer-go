package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"web3-indexer-go/internal/engine"
)

// handleStressColliderStart 启动压测
func handleStressColliderStart(w http.ResponseWriter, r *http.Request) {
	// 只有 Anvil 模式允许压测
	if cfg.ChainID != 31337 {
		http.Error(w, `{"error":"StressCollider only available in Anvil mode (chainID=31337)"}`, http.StatusForbidden)
		return
	}

	// 解析参数
	targetTPS, _ := strconv.Atoi(r.URL.Query().Get("tps"))
	if targetTPS <= 0 {
		targetTPS = 1000 // 默认 1K TPS
	}
	if targetTPS > 5000 {
		targetTPS = 5000 // 上限保护
	}

	durationSec, _ := strconv.Atoi(r.URL.Query().Get("duration"))
	if durationSec <= 0 {
		durationSec = 300 // 默认 5 分钟
	}
	if durationSec > 3600 {
		durationSec = 3600 // 最长 1 小时
	}

	complexity := r.URL.Query().Get("complexity")
	if complexity == "" {
		complexity = "defi"
	}

	// 检查是否已有运行中的压测
	existing := engine.GetGlobalStressCollider()
	if existing != nil {
		// 如果已有，尝试停止旧的
		existing.Stop()
		time.Sleep(100 * time.Millisecond)
	}

	// 创建新配置
	config := engine.ColliderConfig{
		TargetTPS:       targetTPS,
		BurstSize:       50,
		RampingPeriod:   10 * time.Second,
		TxComplexity:    complexity,
		ContractDensity: 0.1,
		AccountCount:    100,
		BatchMode:       true,
		Duration:        time.Duration(durationSec) * time.Second,
		MaxPendingTx:    10,
	}

	// 创建并启动
	collider, err := engine.NewStressCollider(cfg.RPCURLs[0], config)
	if err != nil {
		slog.Error("❌ [STRESS_COLLIDER] 启动失败", "err", err)
		http.Error(w, `{"error":"Failed to start stress collider: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	engine.SetGlobalStressCollider(collider)
	collider.Start()

	resp := map[string]interface{}{
		"status":     "started",
		"target_tps": targetTPS,
		"duration":   durationSec,
		"complexity": complexity,
		"chain_id":   cfg.ChainID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	slog.Info("🔥 [STRESS_COLLIDER] 压测启动", "tps", targetTPS, "duration", durationSec, "complexity", complexity)
}

// handleStressColliderStop 停止压测
func handleStressColliderStop(w http.ResponseWriter, r *http.Request) {
	collider := engine.GetGlobalStressCollider()
	if collider == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "not_running",
			"message": "No active stress test found",
		})
		return
	}

	// 获取最终指标
	metrics := collider.GetMetricsSnapshot()

	collider.Stop()
	engine.StopGlobalStressCollider()

	resp := map[string]interface{}{
		"status":  "stopped",
		"metrics": metrics,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	slog.Info("🛑 [STRESS_COLLIDER] 压测停止")
}

// handleStressColliderMetrics 获取压测指标
func handleStressColliderMetrics(w http.ResponseWriter, r *http.Request) {
	collider := engine.GetGlobalStressCollider()
	if collider == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "not_running",
			"message": "No active stress test",
		})
		return
	}

	// 获取实时指标
	metrics := collider.GetMetricsSnapshot()

	// 构建响应
	resp := map[string]interface{}{
		"status":     "running",
		"metrics":    metrics,
		"target_tps": collider.GetConfig().TargetTPS,
		"chain_id":   cfg.ChainID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
