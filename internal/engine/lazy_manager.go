package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

const (
	ModeActive = "active"
	ModeSleep  = "sleep"
)

// LazyManager manages the indexing state based on activity
type LazyManager struct {
	mu             sync.RWMutex
	isActive       bool
	isAlwaysActive bool // 🚀 新增：强制活跃模式（用于实验室环境）
	lastHeartbeat  time.Time
	lastBlockTime  time.Time // 🔥 新增：最后一次处理区块的时间（活动双重校验）
	timeout        time.Duration
	fetcher        *Fetcher
	rpcPool        RPCClient
	logger         *slog.Logger
	guard          *ConsistencyGuard                   // 🛡️ Linearity Guard
	OnStatus       func(status map[string]interface{}) // 🚀 Callback for status changes

	// stateManager coordinates with the higher-level StateManager so that
	// LazyManager.Trigger() and StateManager.watchdog() do not race.
	// When set, Trigger() calls stateManager.RecordAccess() instead of
	// directly resuming the fetcher, and the sleep transition delegates
	// to stateManager.transitionTo(StateIdle).
	stateManager *StateManager

	// 🚀 配置监控周期
	monitorInterval time.Duration
	regressInterval time.Duration
}

// NewLazyManager creates a new LazyManager instance with a heartbeat timeout
func NewLazyManager(fetcher *Fetcher, rpcPool RPCClient, timeout time.Duration, guard *ConsistencyGuard) *LazyManager {
	lm := &LazyManager{
		isActive:        false,
		lastHeartbeat:   time.Now().Add(-timeout), // Initialize as inactive
		timeout:         timeout,
		fetcher:         fetcher,
		rpcPool:         rpcPool,
		guard:           guard,
		logger:          slog.Default(),
		monitorInterval: 30 * time.Second,
		regressInterval: 60 * time.Second,
	}

	// Initial state: ensure fetcher is paused
	fetcher.Pause()

	return lm
}

// SetStateManager registers the higher-level StateManager so that LazyManager
// routes wake/sleep transitions through it instead of directly manipulating
// the fetcher. This eliminates the P0-1 race where StateManager.Stop() and
// LazyManager.fetcher.Resume() execute concurrently without coordination.
func (lm *LazyManager) SetStateManager(sm *StateManager) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.stateManager = sm
}

// SetAlwaysActive 开启强制活跃模式，屏蔽休眠逻辑
func (lm *LazyManager) SetAlwaysActive(enabled bool) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.isAlwaysActive = enabled
	if enabled {
		lm.isActive = true
		lm.fetcher.Resume()
		lm.logger.Info("🔥 LAB_MODE: Hibernation disabled. Engine is roaring.")
	}
}

// 🔥 NotifyBlockProcessed 通知 LazyManager 有新区块被处理（活动双重校验）
// 这个方法由 Processor 在每次处理完区块后调用，确保即使没有用户交互，
// 只要有区块链活动，系统也不会进入休眠
func (lm *LazyManager) NotifyBlockProcessed(blockNum int64) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.lastBlockTime = time.Now()

	// 如果系统处于休眠状态，但有新区块处理，立即唤醒
	if !lm.isActive && !lm.isAlwaysActive {
		lm.isActive = true
		lm.logger.Info("🔥 BLOCK_ACTIVITY_DETECTED: Waking up from block processing",
			"block", blockNum)

		if lm.stateManager != nil {
			go lm.stateManager.RecordAccess()
		} else {
			lm.fetcher.Resume()
		}

		if lm.OnStatus != nil {
			go lm.OnStatus(lm.getStatusLocked())
		}
	}
}

// Trigger (Heartbeat) activates indexing if currently inactive
func (lm *LazyManager) Trigger() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.isAlwaysActive {
		return // 强制活跃模式下忽略触发
	}

	lm.lastHeartbeat = time.Now()
	if !lm.isActive {
		lm.isActive = true
		lm.logger.Info("🚀 ACTIVITY DETECTED: Waking up indexer", "timeout", lm.timeout)

		// If a StateManager is registered, delegate the wake-up to it so that
		// both state machines stay in sync. StateManager.RecordAccess() will
		// call transitionTo(StateActive) → startActiveMode() → indexer.Start(),
		// which in turn resumes the fetcher through the normal path.
		if lm.stateManager != nil {
			go lm.stateManager.RecordAccess()
			if lm.OnStatus != nil {
				go lm.OnStatus(lm.getStatusLocked())
			}
			return
		}

		// Standalone mode (no StateManager): manage fetcher directly.
		// 🔧 使用统一的 goroutine 模式，确保所有操作都有超时保护
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if lm.guard != nil {
				if err := lm.guard.PerformLinearityCheck(ctx); err != nil {
					lm.logger.Error("wake_up_linearity_check_failed", "err", err)
				}
			}

			// fetcher.Resume() 也受 30 秒超时保护（通过同一个 context）
			lm.fetcher.Resume()
		}()

		if lm.OnStatus != nil {
			go lm.OnStatus(lm.getStatusLocked())
		}

		// 🔥 SSOT: 通过 Orchestrator 广播 Wake 事件（单一控制面）
		orchestrator := GetOrchestrator()
		if orchestrator != nil {
			orchestrator.RecordUserActivity()
		}
	}
}

// StartMonitor starts a background loop to check for inactivity and regression
func (lm *LazyManager) StartMonitor(ctx context.Context) {
	go func() {
		// 🚀 工业级监控周期：动态可配
		ticker := time.NewTicker(lm.monitorInterval)
		regressTicker := time.NewTicker(lm.regressInterval)
		defer ticker.Stop()
		defer regressTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				lm.mu.Lock()
				// 🛡️ 强制活跃模式下跳过休眠判定
				if !lm.isAlwaysActive && lm.isActive && time.Since(lm.lastHeartbeat) > lm.timeout {
					// 🔥 强制同步检查：如果有显著 SyncLag，禁止进入休眠
					// Data completeness beats quota saving.
					snap := GetHeightOracle().Snapshot()
					currentLag := snap.ChainHead - snap.IndexedHead
					if currentLag < 0 {
						currentLag = 0 // 时间旅行场景
					}

					// 🔥 横滨实验室强化：任何 Lag > 10 都禁止休眠
					if currentLag > 10 {
						lm.logger.Warn("🚫 ECO_SLEEP_BLOCKED: SyncLag too large, staying active",
							"sync_lag", currentLag,
							"chain_head", snap.ChainHead,
							"indexed_head", snap.IndexedHead,
							"min_lag_to_sleep", 10)
						lm.mu.Unlock()
						continue
					}

					lm.isActive = false
					lm.logger.Info("💤 INACTIVITY DETECTED: Entering sleep mode to save RPC quota",
						"sync_lag", currentLag,
						"chain_head", snap.ChainHead,
						"indexed_head", snap.IndexedHead)

					sm := lm.stateManager
					if lm.OnStatus != nil {
						go lm.OnStatus(lm.getStatusLocked())
					}
					lm.mu.Unlock()

					// Coordinate with StateManager when present: let it drive the
					// transition so indexer.Stop() and fetcher.Pause() happen in
					// the correct order through a single code path.
					if sm != nil {
						sm.transitionTo(StateIdle)
					} else {
						lm.fetcher.Pause()
					}
					continue
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
	if lm.isAlwaysActive {
		status["mode"] = ModeActive
		status["display"] = "🔥 Lab Mode: Engine Roaring"
		status["is_lab_mode"] = true
		return status
	}

	// 🔥 活动双重校验：只要有用户活动 OR 区块链活动，就认为是活跃状态
	lastActivity := lm.lastHeartbeat
	if lm.lastBlockTime.After(lastActivity) {
		lastActivity = lm.lastBlockTime
	}

	timeSinceActivity := time.Since(lastActivity)
	isActiveDueToBlocks := lm.lastBlockTime.After(lm.lastHeartbeat)

	if lm.isActive || isActiveDueToBlocks {
		remaining := lm.timeout - timeSinceActivity
		status["mode"] = ModeActive
		if isActiveDueToBlocks {
			status["display"] = "🔥 Active (Block Processing)"
			status["activity_source"] = "blockchain"
		} else {
			status["display"] = "● Active (User Activity)"
			status["activity_source"] = "user"
		}
		status["sleep_in"] = int(remaining.Seconds())
	} else {
		status["mode"] = ModeSleep
		status["display"] = "● Eco-Mode: Quota Protection Active"
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
