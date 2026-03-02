package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

var (
	orchestrator     *Orchestrator
	orchestratorOnce sync.Once
)

// GetOrchestrator 返回协调器单例
func GetOrchestrator() *Orchestrator {
	orchestratorOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		orchestrator = &Orchestrator{
			cmdChan: make(chan Message, 50000), // 🚀 16G RAM 调优：适中缓冲区
			state: CoordinatorState{
				UpdatedAt:        time.Now(),
				SystemState:      SystemStateUnknown,
				LastUserActivity: time.Now(), // 初始化为当前时间
				SafetyBuffer:     1,          // 初始保持 1 个块的距离
			},
			broadcastCh:         make(chan CoordinatorState, 1000),
			subscribers:         make([]chan CoordinatorState, 0, 8),
			ctx:                 ctx,
			cancel:              cancel,
			enableProfiling:     true,
			isYokohamaLab:       false, // 稍后在 Init() 中检测
			pendingHeightUpdate: nil,
			lastHeightMergeTime: time.Now(),
		}
		go orchestrator.loop()
		go orchestrator.broadcaster()
		slog.Info("🎼 Orchestrator SSOT initialized", "channel_depth", 100000)
	})
	return orchestrator
}

// Init 初始化协调器（设置环境感知配置）
func (o *Orchestrator) Init(_ context.Context, fetcher *Fetcher, strategy Strategy) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.fetcher = fetcher
	o.strategy = strategy
	o.state.SafetyBuffer = strategy.GetInitialSafetyBuffer()

	slog.Info("🎼 Orchestrator initialized", "strategy", strategy.Name(), "safety_buffer", o.state.SafetyBuffer)
}

// LoadInitialState 从数据库加载初始状态
func (o *Orchestrator) LoadInitialState(db *sqlx.DB, chainID int64) error {
	var lastSyncedBlock string
	err := db.GetContext(context.Background(), &lastSyncedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)

	// 🚀 增强逻辑：如果 checkpoint 没找到，尝试从 blocks 表直接获取最大值
	if err != nil || lastSyncedBlock == "" || lastSyncedBlock == "0" {
		var maxInDB int64
		err = db.GetContext(context.Background(), &maxInDB, "SELECT COALESCE(MAX(number), 0) FROM blocks")
		if err == nil && maxInDB > 0 {
			lastSyncedBlock = fmt.Sprintf("%d", maxInDB)
		}
	}

	if lastSyncedBlock != "" && lastSyncedBlock != "0" {
		height, ok := new(big.Int).SetString(lastSyncedBlock, 10)
		if ok {
			o.mu.Lock()
			o.state.SyncedCursor = height.Uint64()
			o.snapshot = o.state
			o.mu.Unlock()
			slog.Info("🎼 Orchestrator: Initial state aligned from DB", "cursor", lastSyncedBlock)
		}
	}
	return nil
}

// SetAsyncWriter 设置异步写入器（用于异步持久化）
func (o *Orchestrator) SetAsyncWriter(writer *AsyncWriter) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.asyncWriter = writer
	slog.Info("🎼 Orchestrator: AsyncWriter linked")
}

// RestoreState 恢复状态（用于检查点热启动）
func (o *Orchestrator) RestoreState(state CoordinatorState) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.state = state
	o.snapshot = state

	slog.Info("🎼 Orchestrator state restored",
		"height", state.SyncedCursor,
		"transfers", state.Transfers,
		"eco_mode", state.IsEcoMode)
}

// SnapToReality 强制将内存位点对齐到链尖（用于解决幽灵位点问题）
func (o *Orchestrator) SnapToReality(rpcHeight uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.state.LatestHeight > rpcHeight+1000 {
		slog.Warn("🎼 Orchestrator: Ghost state detected! Snapping to reality", "ghost", o.state.LatestHeight, "real", rpcHeight)
		o.state.LatestHeight = rpcHeight
		o.state.FetchedHeight = rpcHeight
		o.state.SyncedCursor = rpcHeight
		o.state.TargetHeight = rpcHeight
		o.snapshot = o.state
	}
}

// ForceSetCursors 强制设置所有游标到指定高度（用于 Leap-Sync 和死锁看门狗）
func (o *Orchestrator) ForceSetCursors(height uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()

	slog.Warn("🎼 Orchestrator: Force setting cursors", "new_height", height)
	o.state.LatestHeight = height
	o.state.FetchedHeight = height
	o.state.SyncedCursor = height
	o.state.TargetHeight = height
	o.snapshot = o.state
	
	// 如果配置了 Fetcher，也必须清空任务队列并重置
	if o.fetcher != nil {
		o.fetcher.ClearJobs()
	}
}

// ResetToZero 强制归零游标 (用于全内存模式或 Anvil 重置)
func (o *Orchestrator) ResetToZero() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state.SyncedCursor = 0
	o.state.FetchedHeight = 0
	o.state.LatestHeight = 0
	o.state.TargetHeight = 0
	o.snapshot = o.state

	// 🚀 同时清空 Fetcher 队列，防止老任务干扰新周期
	if o.fetcher != nil {
		o.fetcher.ClearJobs()
	}

	slog.Warn("🎼 Orchestrator: State reset to zero (EPHEMERAL_MODE)")
}

// Reset 重置协调器状态（仅用于测试）
func (o *Orchestrator) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	safetyBuffer := uint64(1)
	if o.strategy != nil {
		safetyBuffer = o.strategy.GetInitialSafetyBuffer()
	}

	o.state = CoordinatorState{
		UpdatedAt:        time.Now(),
		SystemState:      SystemStateUnknown,
		LastUserActivity: time.Now(),
		SafetyBuffer:     safetyBuffer,
	}
	o.snapshot = o.state
	slog.Info("🎼 Orchestrator: State reset for testing")
}

// Shutdown 优雅关闭协调器
func (o *Orchestrator) Shutdown() {
	slog.Info("orchestrator_shutting_down")
	o.cancel()

	// 关闭异步写入器
	if o.asyncWriter != nil {
		if err := o.asyncWriter.Shutdown(30 * time.Second); err != nil {
			slog.Error("async_writer_shutdown_failed", "err", err)
		}
	}
}
