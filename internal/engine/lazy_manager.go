package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// LazyManager manages the indexing state based on activity
type LazyManager struct {
	mu            sync.RWMutex
	isActive      bool
	lastHeartbeat time.Time
	timeout       time.Duration
	fetcher       *Fetcher
	rpcPool       RPCClient
	logger        *slog.Logger
	guard         *ConsistencyGuard                   // 🛡️ Linearity Guard
	OnStatus      func(status map[string]interface{}) // 🚀 Callback for status changes
}

// NewLazyManager creates a new LazyManager instance with a heartbeat timeout
func NewLazyManager(fetcher *Fetcher, rpcPool RPCClient, timeout time.Duration, guard *ConsistencyGuard) *LazyManager {
	lm := &LazyManager{
		isActive:      false,
		lastHeartbeat: time.Now().Add(-timeout), // Initialize as inactive
		timeout:       timeout,
		fetcher:       fetcher,
		rpcPool:       rpcPool,
		guard:         guard,
		logger:        slog.Default(),
	}

	// Initial state: ensure fetcher is paused
	fetcher.Pause()

	return lm
}

// Trigger (Heartbeat) activates indexing if currently inactive
func (lm *LazyManager) Trigger() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.lastHeartbeat = time.Now()
	if !lm.isActive {
		lm.isActive = true
		lm.logger.Info("🚀 ACTIVITY DETECTED: Waking up indexer", "timeout", lm.timeout)

		// 🛡️ 工业级对齐：唤醒瞬间执行线性检查，防止休眠期间环境已重置
		if lm.guard != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				// 💡 状态上报逻辑已由 initEngine 中的 OnStatus 闭包处理
				if err := lm.guard.PerformLinearityCheck(ctx); err != nil {
					lm.logger.Error("wake_up_linearity_check_failed", "err", err)
				}
				lm.fetcher.Resume()
			}()
		} else {
			lm.fetcher.Resume()
		}

		if lm.OnStatus != nil {
			go lm.OnStatus(lm.getStatusLocked())
		}
	}
}

// StartMonitor starts a background loop to check for inactivity and regression
func (lm *LazyManager) StartMonitor(ctx context.Context) {
	go func() {
		// 🚀 工业级监控周期：30秒检查一次活跃度，60秒执行一次回归预警
		ticker := time.NewTicker(30 * time.Second)
		regressTicker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		defer regressTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				lm.mu.Lock()
				if lm.isActive && time.Since(lm.lastHeartbeat) > lm.timeout {
					lm.isActive = false
					lm.logger.Info("💤 INACTIVITY DETECTED: Entering sleep mode to save RPC quota")
					lm.fetcher.Pause()
					if lm.OnStatus != nil {
						go lm.OnStatus(lm.getStatusLocked())
					}
				}
				lm.mu.Unlock()

			case <-regressTicker.C:
				// 🛡️ Regressive Watchdog: 即使在活跃状态，也要检查是否发生了环境回滚
				lm.mu.RLock()
				active := lm.isActive
				lm.mu.RUnlock()

				if active && lm.guard != nil {
					// 💡 执行轻量级回归检查，无需加锁
					go func() {
						regressCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						if err := lm.guard.PerformLinearityCheck(regressCtx); err != nil {
							lm.logger.Error("background_regression_check_failed", "err", err)
						}
					}()
				}
			}
		}
	}()
}

// getStatusLocked returns status without acquiring lock (internal use)
func (lm *LazyManager) getStatusLocked() map[string]interface{} {
	status := make(map[string]interface{})
	if lm.isActive {
		remaining := lm.timeout - time.Since(lm.lastHeartbeat)
		status["mode"] = "active"
		status["display"] = "● 活跃中 (Active)"
		status["sleep_in"] = int(remaining.Seconds())
	} else {
		status["mode"] = "sleep"
		status["display"] = "● 睡眠中 (Saving Quota)"
	}
	return status
}

// GetStatus returns the current status of the lazy indexer
func (lm *LazyManager) GetStatus() map[string]interface{} {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.getStatusLocked()
}

// DBInterface defines the minimal database interface needed for LazyManager
type DBInterface interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}
