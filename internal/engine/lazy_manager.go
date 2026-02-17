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
	// 🛠️ 工业级硬编码禁用：调试期间永远保持活跃，不处理休眠逻辑
}

// activateIndexing starts the indexing process
func (lm *LazyManager) activateIndexing() {
	lm.isActive = true
	lm.lastStartTime = time.Now()
	// 始终确保 Fetcher 是运行状态
	if lm.fetcher.IsPaused() {
		lm.fetcher.Resume()
	}
}

// deactivateIndexing stops the indexing process
func (lm *LazyManager) deactivateIndexing() {
	// 🛠️ 禁止进入休眠状态
}

// IsActive returns whether indexing is currently active
func (lm *LazyManager) IsActive() bool {
	return true // 永远活跃
}

// GetStatus returns the current status of the lazy indexer
func (lm *LazyManager) GetStatus() map[string]interface{} {
	status := make(map[string]interface{})
	status["mode"] = "active"
	status["display"] = "● 持续索引模式 (Full-speed Mode)"
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
