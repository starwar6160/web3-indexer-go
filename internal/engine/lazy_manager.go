package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// LazyManager manages the indexing state and cooldown periods
type LazyManager struct {
	mu            sync.RWMutex
	isActive      bool
	lastStartTime time.Time
	stopTimer     *time.Timer
	cooldown      time.Duration
	activePeriod  time.Duration
	fetcher       *Fetcher
	rpcPool       RPCClient
}

// NewLazyManager creates a new LazyManager instance
func NewLazyManager(fetcher *Fetcher, rpcPool RPCClient, cooldown time.Duration, activePeriod time.Duration) *LazyManager {
	return &LazyManager{
		isActive:      false,
		lastStartTime: time.Now().Add(-cooldown), // Initialize with cooldown elapsed
		cooldown:      cooldown,
		activePeriod:  activePeriod,
		fetcher:       fetcher,
		rpcPool:       rpcPool,
	}
}

// Trigger activates indexing if cooldown period has passed
func (lm *LazyManager) Trigger() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()

	// Check if we're still in cooldown period
	if now.Sub(lm.lastStartTime) < lm.cooldown && lm.isActive {
		// Still in active period, do nothing
		return
	}

	// Check if we're past the cooldown period
	if !lm.isActive && now.Sub(lm.lastStartTime) > lm.cooldown {
		lm.activateIndexing()
	} else if lm.isActive {
		// Already active, extend the timer
		if lm.stopTimer != nil {
			lm.stopTimer.Stop()
		}
		lm.setupStopTimer()
	}
}

// activateIndexing starts the indexing process
func (lm *LazyManager) activateIndexing() {
	lm.isActive = true
	lm.lastStartTime = time.Now()
	slog.Info("🚀 访客触发：开始限时索引（正在追赶中...）",
		slog.Duration("active_period", lm.activePeriod),
		slog.Duration("cooldown_period", lm.cooldown))

	// Resume the fetcher if it was paused
	if lm.fetcher.IsPaused() {
		lm.fetcher.Resume()
	}

	// Setup timer to stop indexing after active period
	lm.setupStopTimer()
}

// setupStopTimer creates a timer to stop indexing after the active period
func (lm *LazyManager) setupStopTimer() {
	if lm.stopTimer != nil {
		lm.stopTimer.Stop()
	}

	lm.stopTimer = time.AfterFunc(lm.activePeriod, func() {
		lm.deactivateIndexing()
	})
}

// deactivateIndexing stops the indexing process
func (lm *LazyManager) deactivateIndexing() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if !lm.isActive {
		return
	}

	lm.isActive = false
	slog.Info("💤 任务完成：进入懒惰模式，暂停索引以节省额度")

	// Pause the fetcher to stop indexing
	lm.fetcher.Pause()
}

// IsActive returns whether indexing is currently active
func (lm *LazyManager) IsActive() bool {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.isActive
}

// GetStatus returns the current status of the lazy indexer
func (lm *LazyManager) GetStatus() map[string]interface{} {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	now := time.Now()
	status := make(map[string]interface{})

	if lm.isActive {
		remainingTime := lm.activePeriod - now.Sub(lm.lastStartTime)
		if remainingTime < 0 {
			remainingTime = 0
		}

		status["mode"] = "active"
		status["display"] = "● 正在追赶中 (Catching up...)"
		status["remaining_time"] = remainingTime.String()
	} else {
		status["mode"] = "lazy"
		status["display"] = "● 节能模式 (Lazy Mode)"

		if !lm.lastStartTime.IsZero() {
			timeSinceEnd := now.Sub(lm.lastStartTime.Add(lm.cooldown))
			if timeSinceEnd < 0 {
				status["cooldown_remaining"] = (-timeSinceEnd).String()
			} else {
				status["status"] = "ready_to_activate"
			}
		}
	}

	return status
}

// StartInitialIndexing starts the initial indexing period on startup
func (lm *LazyManager) StartInitialIndexing() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.activateIndexing()
}

// StartHeartbeat starts the heartbeat mechanism to keep chain head updated
func (lm *LazyManager) StartHeartbeat(ctx context.Context, db DBInterface, chainID int64) {
	// 定义更新逻辑，以便复用
	updateFunc := func() {
		latestChainBlock, err := lm.rpcPool.GetLatestBlockNumber(ctx)
		if err != nil {
			slog.Error("failed_to_get_latest_block_for_heartbeat", "err", err)
			return
		}

		_, err = db.ExecContext(ctx,
			"INSERT INTO sync_checkpoints (chain_id, last_synced_block, updated_at) VALUES ($1, $2, NOW()) "+
				"ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = $2, updated_at = NOW()",
			chainID,
			latestChainBlock.String())
		if err != nil {
			slog.Error("failed_to_update_chain_head_checkpoint", "err", err)
		}
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("lazy_manager_goroutine_panic", "err", r)
			}
		}()
		// 🚀 6.1 优化：启动时立即执行一次预热
		updateFunc()

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateFunc()
			}
		}
	}()
}

// DeactivateIndexingForced forces deactivation of indexing without checking conditions
func (lm *LazyManager) DeactivateIndexingForced() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if !lm.isActive {
		return
	}

	lm.isActive = false
	slog.Info("💤 FORCED PAUSE: Entering lazy mode to save quota")

	// Pause the fetcher to stop indexing
	lm.fetcher.Pause()
}

// DBInterface defines the minimal database interface needed for LazyManager
type DBInterface interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}
