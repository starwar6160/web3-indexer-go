package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

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

// SetSequencer 动态设置 sequencer（用于 main 初始化后注入）
func (h *HealthServer) SetSequencer(sequencer *Sequencer) {
	h.sequencer = sequencer
}