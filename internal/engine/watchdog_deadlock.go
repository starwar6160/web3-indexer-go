package engine

import (
	"context"
	"log/slog"
	"time"
)

// HealingEvent 表示自愈事件的详细信息（用于 WebSocket 推送）
type HealingEvent struct {
	TriggerReason string  `json:"trigger_reason"` // "space_time_tear"
	DBWatermark   int64   `json:"db_watermark"`   // 数据库水位线
	RPCHeight     int64   `json:"rpc_height"`     // RPC 实际高度
	GapSize       int64   `json:"gap_size"`       // 断层大小
	Success       bool    `json:"success"`        // 是否成功
	Error         string  `json:"error,omitempty"` // 错误信息（如果失败）
}

// DeadlockWatchdog 二阶状态审计看门狗，专门解决"时空撕裂"导致的死锁
type DeadlockWatchdog struct {
	enabled    bool
	chainID    int64
	demoMode   bool
	stallThreshold time.Duration // 120秒闲置阈值
	checkInterval  time.Duration // 30秒检查周期

	sequencer   *Sequencer
	repo        RepositoryAdapter
	rpcPool     RPCClient
	lazyManager *LazyManager
	metrics     *Metrics

	// 回调函数
	OnHealingTriggered func(event HealingEvent)

	ctx    context.Context
	cancel context.CancelFunc
}

// RepositoryAdapter 定义看门狗需要的数据库接口
type RepositoryAdapter interface {
	UpdateSyncCursor(ctx context.Context, height int64) error
	GetMaxStoredBlock(ctx context.Context) (int64, error)
}

// NewDeadlockWatchdog 创建新的死锁看门狗实例
func NewDeadlockWatchdog(
	chainID int64,
	demoMode bool,
	sequencer *Sequencer,
	repo RepositoryAdapter,
	rpcPool RPCClient,
	lazyManager *LazyManager,
	metrics *Metrics,
) *DeadlockWatchdog {
	return &DeadlockWatchdog{
		chainID:        chainID,
		demoMode:       demoMode,
		stallThreshold: 120 * time.Second,
		checkInterval:  30 * time.Second,
		sequencer:      sequencer,
		repo:           repo,
		rpcPool:        rpcPool,
		lazyManager:    lazyManager,
		metrics:        metrics,
		enabled:        false, // 默认禁用，需要调用 Enable()
	}
}

// Enable 启用看门狗
func (dw *DeadlockWatchdog) Enable() {
	// 🔒 环境隔离：仅在 Anvil 或演示模式下启用
	if dw.chainID != 31337 && !dw.demoMode {
		Logger.Warn("🔒 DeadlockWatchdog: Environment check failed - not enabling",
			slog.Int64("chain_id", dw.chainID),
			slog.Bool("demo_mode", dw.demoMode))
		return
	}

	dw.enabled = true
	Logger.Info("🛡️ DeadlockWatchdog: Enabled",
		slog.Int64("chain_id", dw.chainID),
		slog.Bool("demo_mode", dw.demoMode),
		slog.Duration("stall_threshold", dw.stallThreshold),
		slog.Duration("check_interval", dw.checkInterval))
}

// Start 启动看门狗协程
func (dw *DeadlockWatchdog) Start(ctx context.Context) {
	if !dw.enabled {
		Logger.Debug("DeadlockWatchdog: Not enabled, skipping start")
		return
	}

	dw.ctx, dw.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(dw.checkInterval)
		defer ticker.Stop()

		Logger.Info("🛡️ DeadlockWatchdog: Started background monitoring")

		for {
			select {
			case <-dw.ctx.Done():
				Logger.Info("🛡️ DeadlockWatchdog: Stopped")
				return
			case <-ticker.C:
				if err := dw.checkAndHeal(dw.ctx); err != nil {
					Logger.Warn("DeadlockWatchdog: Check failed",
						slog.String("error", err.Error()))
				}
			}
		}
	}()
}

// Stop 停止看门狗
func (dw *DeadlockWatchdog) Stop() {
	if dw.cancel != nil {
		dw.cancel()
	}
}

// checkAndHeal 执行死锁检测和自愈
func (dw *DeadlockWatchdog) checkAndHeal(ctx context.Context) error {
	if !dw.enabled {
		return nil
	}

	// Step 1: 检测闲置时间
	idleTime := dw.sequencer.GetIdleTime()
	if idleTime < dw.stallThreshold {
		// 未达到闲置阈值，继续监控
		return nil
	}

	Logger.Warn("🛡️ DeadlockWatchdog: Stall detected",
		slog.Duration("idle_time", idleTime),
		slog.Duration("threshold", dw.stallThreshold))

	// Step 2: 获取真实状态（不受 Sequencer 影响）
	rpcHeight, err := dw.rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		Logger.Warn("DeadlockWatchdog: Failed to get RPC height",
			slog.String("error", err.Error()))
		return err
	}

	dbHeight, err := dw.repo.GetMaxStoredBlock(ctx)
	if err != nil {
		Logger.Warn("DeadlockWatchdog: Failed to get DB height",
			slog.String("error", err.Error()))
		return err
	}

	sequencerExpected := dw.sequencer.GetExpectedBlock()

	Logger.Info("🛡️ DeadlockWatchdog: State snapshot",
		slog.Int64("rpc_height", rpcHeight.Int64()),
		slog.Int64("db_watermark", dbHeight),
		slog.String("sequencer_expected", sequencerExpected.String()),
		slog.Duration("idle_time", idleTime))

	// Step 3: 判断是否为"时空撕裂"
	gapSize := rpcHeight.Int64() - dbHeight
	isSpaceTimeTear := gapSize > 1000 && sequencerExpected.Int64() < rpcHeight.Int64()-1000

	if !isSpaceTimeTear {
		// 不是时空撕裂，可能只是正常延迟
		Logger.Debug("DeadlockWatchdog: Not a space-time tear, skipping",
			slog.Int64("gap_size", gapSize),
			slog.Bool("is_space_time_tear", isSpaceTimeTear))
		return nil
	}

	// 🚨 检测到时空撕裂！执行三步自愈
	Logger.Error("🚨 DeadlockWatchdog: SPACE-TIME TEAR DETECTED",
		slog.Int64("db_watermark", dbHeight),
		slog.Int64("rpc_height", rpcHeight.Int64()),
		slog.Int64("gap_size", gapSize),
		slog.String("sequencer_expected", sequencerExpected.String()))

	// 记录指标
	if dw.metrics != nil && dw.metrics.SelfHealingTriggered != nil {
		dw.metrics.SelfHealingTriggered.Inc()
	}

	event := HealingEvent{
		TriggerReason: "space_time_tear",
		DBWatermark:   dbHeight,
		RPCHeight:     rpcHeight.Int64(),
		GapSize:       gapSize,
		Success:       false,
	}

	// 🔧 Step 1/3: 物理级游标强插（数据库）
	newCursorHeight := rpcHeight.Int64() - 1
	Logger.Info("🔧 DeadlockWatchdog: Step 1/3: Physical cursor force-insert",
		slog.Int64("new_cursor", newCursorHeight))

	if err := dw.repo.UpdateSyncCursor(ctx, newCursorHeight); err != nil {
		Logger.Error("❌ DeadlockWatchdog: Step 1 FAILED",
			slog.String("error", err.Error()))
		event.Error = "Step 1 failed: " + err.Error()
		dw.notifyHealingEvent(event)
		if dw.metrics != nil && dw.metrics.SelfHealingFailure != nil {
			dw.metrics.SelfHealingFailure.Inc()
		}
		return err
	}

	// 🔧 Step 2/3: 状态机热重启（Sequencer）
	Logger.Info("🔧 DeadlockWatchdog: Step 2/3: State machine hot restart",
		slog.Int64("reset_to", rpcHeight.Int64()))

	dw.sequencer.ResetExpectedBlock(rpcHeight)

	// 🔧 Step 3/3: Buffer 清理
	Logger.Info("🔧 DeadlockWatchdog: Step 3/3: Buffer cleanup")
	dw.sequencer.ClearBuffer()

	// ✅ 自愈成功
	event.Success = true
	dw.notifyHealingEvent(event)

	Logger.Info("✅ DeadlockWatchdog: Self-healing SUCCESS",
		slog.Int64("old_db_watermark", dbHeight),
		slog.Int64("new_cursor", newCursorHeight),
		slog.Int64("sequencer_reset_to", rpcHeight.Int64()))

	if dw.metrics != nil && dw.metrics.SelfHealingSuccess != nil {
		dw.metrics.SelfHealingSuccess.Inc()
	}

	return nil
}

// notifyHealingEvent 通知自愈事件（WebSocket 回调）
func (dw *DeadlockWatchdog) notifyHealingEvent(event HealingEvent) {
	if dw.OnHealingTriggered != nil {
		// 在新协程中调用，避免阻塞看门狗
		go func() {
			dw.OnHealingTriggered(event)
		}()
	}
}
