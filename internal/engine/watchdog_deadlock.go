package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"
)

// HealingEvent 表示自愈事件的详细信息（用于 WebSocket 推送）
type HealingEvent struct {
	TriggerReason string `json:"trigger_reason"`  // "space_time_tear"
	DBWatermark   int64  `json:"db_watermark"`    // 数据库水位线
	RPCHeight     int64  `json:"rpc_height"`      // RPC 实际高度
	GapSize       int64  `json:"gap_size"`        // 断层大小
	Success       bool   `json:"success"`         // 是否成功
	Error         string `json:"error,omitempty"` // 错误信息（如果失败）
}

// DeadlockWatchdog 二阶状态审计看门狗，专门解决"时空撕裂"导致的死锁
type DeadlockWatchdog struct {
	enabled        bool
	chainID        int64
	demoMode       bool
	stallThreshold time.Duration // 120秒闲置阈值
	checkInterval  time.Duration // 30秒检查周期
	gapThreshold   int64         // 触发自愈的最小 block gap（可通过 SetGapThreshold 调整）

	sequencer   *Sequencer
	fetcher     *Fetcher // used to reschedule the gap range after healing
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
	GetSyncCursor(ctx context.Context) (int64, error)
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
		gapThreshold:   1000, // default: trigger self-healing when gap > 1000 blocks
		sequencer:      sequencer,
		repo:           repo,
		rpcPool:        rpcPool,
		lazyManager:    lazyManager,
		metrics:        metrics,
		enabled:        false, // 默认禁用，需要调用 Enable()
	}
}

// SetFetcher wires the Fetcher so the watchdog can reschedule the gap range
// after a successful self-heal. Without this, UpdateSyncCursor moves the
// cursor in sync_checkpoints but the blocks table stays at the old watermark
// because no fetch jobs are queued for the skipped range.
func (dw *DeadlockWatchdog) SetFetcher(f *Fetcher) {
	dw.fetcher = f
}

// SetGapThreshold overrides the block-gap size that triggers self-healing.
// Use a lower value (e.g. 500) for fast-block networks like Sepolia.
func (dw *DeadlockWatchdog) SetGapThreshold(blocks int64) {
	if blocks > 0 {
		dw.gapThreshold = blocks
	}
}

// Enable 启用看门狗
// 原先仅允许 Anvil (chainID=31337) 或 demoMode 启用，导致 Sepolia 无自愈保护。
// 现在所有网络均可启用，由调用方决定是否开启。
func (dw *DeadlockWatchdog) Enable() {
	dw.enabled = true
	Logger.Info("🛡️ DeadlockWatchdog: Enabled",
		slog.Int64("chain_id", dw.chainID),
		slog.Bool("demo_mode", dw.demoMode),
		slog.Int64("gap_threshold", dw.gapThreshold),
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

// HealRule 规则链模式接口
type HealRule interface {
	Check(state *HealingState) bool
	Apply(ctx context.Context, dw *DeadlockWatchdog, state *HealingState) error
}

// HealingState 自愈状态快照
type HealingState struct {
	IdleTime          time.Duration
	RPCHeight         int64
	DBHeight          int64
	SequencerExpected int64
	GapSize           int64
	IsSpaceTimeTear   bool
	NewCursorHeight   int64
}

// 1. 闲置检测规则
type IdleCheckRule struct{}

func (r IdleCheckRule) Check(s *HealingState) bool {
	return s.IdleTime >= 120*time.Second
}

func (r IdleCheckRule) Apply(_ context.Context, _ *DeadlockWatchdog, s *HealingState) error {
	Logger.Warn("🛡️ DeadlockWatchdog: Stall detected",
		slog.Duration("idle_time", s.IdleTime))
	return nil
}

// 2. 时空撕裂检测规则
type SpaceTimeTearRule struct{}

func (r SpaceTimeTearRule) Check(s *HealingState) bool {
	return s.IsSpaceTimeTear
}

func (r SpaceTimeTearRule) Apply(_ context.Context, dw *DeadlockWatchdog, s *HealingState) error {
	Logger.Error("🚨 DeadlockWatchdog: SPACE-TIME TEAR DETECTED",
		slog.Int64("db_watermark", s.DBHeight),
		slog.Int64("rpc_height", s.RPCHeight),
		slog.Int64("gap_size", s.GapSize))

	if dw.metrics != nil && dw.metrics.SelfHealingTriggered != nil {
		dw.metrics.SelfHealingTriggered.Inc()
	}
	return nil
}

// 3. 物理游标强插规则
type PhysicalCursorRule struct{}

func (r PhysicalCursorRule) Check(_ *HealingState) bool { return true }

func (r PhysicalCursorRule) Apply(ctx context.Context, dw *DeadlockWatchdog, s *HealingState) error {
	Logger.Info("🔧 DeadlockWatchdog: Step 1/3: Physical cursor force-insert",
		slog.Int64("new_cursor", s.NewCursorHeight))

	if err := dw.repo.UpdateSyncCursor(ctx, s.NewCursorHeight); err != nil {
		Logger.Error("❌ DeadlockWatchdog: Step 1 FAILED", slog.String("error", err.Error()))
		return fmt.Errorf("step 1 failed: %w", err)
	}
	return nil
}

// 4. 状态机热重启规则
type StateMachineRestartRule struct{}

func (r StateMachineRestartRule) Check(_ *HealingState) bool { return true }

func (r StateMachineRestartRule) Apply(_ context.Context, dw *DeadlockWatchdog, s *HealingState) error {
	Logger.Info("🔧 DeadlockWatchdog: Step 2/3: State machine hot restart",
		slog.Int64("reset_to", s.RPCHeight))

	dw.sequencer.ResetExpectedBlock(big.NewInt(s.RPCHeight))
	return nil
}

// 5. Buffer清理规则
type BufferCleanupRule struct{}

func (r BufferCleanupRule) Check(_ *HealingState) bool { return true }

func (r BufferCleanupRule) Apply(_ context.Context, dw *DeadlockWatchdog, _ *HealingState) error {
	Logger.Info("🔧 DeadlockWatchdog: Step 3/3: Buffer cleanup")
	dw.sequencer.ClearBuffer()
	return nil
}
func (dw *DeadlockWatchdog) checkAndHeal(ctx context.Context) error {
	if !dw.enabled {
		return nil
	}

	// 构建自愈规则链
	rules := []HealRule{
		IdleCheckRule{},
		SpaceTimeTearRule{},
		PhysicalCursorRule{},
		StateMachineRestartRule{},
		BufferCleanupRule{},
	}

	// Step 1: 采集状态快照
	state := dw.gatherState(ctx)
	if state == nil {
		return nil // 未达闲置阈值或获取状态失败
	}

	// Step 2: 执行规则链检测
	for _, rule := range rules[:2] { // 先执行检测规则
		if !rule.Check(state) {
			return nil // 不触发自愈
		}
		if err := rule.Apply(ctx, dw, state); err != nil {
			return err
		}
	}

	// Step 3: 执行自愈操作
	event := HealingEvent{
		TriggerReason: "space_time_tear",
		DBWatermark:   state.DBHeight,
		RPCHeight:     state.RPCHeight,
		GapSize:       state.GapSize,
		Success:       false,
	}

	for _, rule := range rules[2:] { // 执行自愈规则
		if err := rule.Apply(ctx, dw, state); err != nil {
			event.Error = err.Error()
			dw.notifyHealingEvent(event)
			if dw.metrics != nil && dw.metrics.SelfHealingFailure != nil {
				dw.metrics.SelfHealingFailure.Inc()
			}
			return err
		}
	}

	// Step 4: 后续处理（SSOT更新、Gap重调度）
	dw.postHeal(ctx, state)

	// ✅ 自愈成功
	event.Success = true
	dw.notifyHealingEvent(event)
	Logger.Info("✅ DeadlockWatchdog: Self-healing SUCCESS",
		slog.Int64("old_db_watermark", state.DBHeight),
		slog.Int64("new_cursor", state.NewCursorHeight),
		slog.Int64("sequencer_reset_to", state.RPCHeight))

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

// gatherState 采集自愈所需状态快照
func (dw *DeadlockWatchdog) gatherState(ctx context.Context) *HealingState {
	idleTime := dw.sequencer.GetIdleTime()
	if idleTime < dw.stallThreshold {
		return nil // 未达闲置阈值
	}

	rpcHeight, err := dw.rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		Logger.Warn("DeadlockWatchdog: Failed to get RPC height", slog.String("error", err.Error()))
		return nil
	}

	dbHeight, err := dw.repo.GetSyncCursor(ctx)
	if err != nil {
		Logger.Warn("DeadlockWatchdog: Failed to get DB cursor", slog.String("error", err.Error()))
		return nil
	}

	sequencerExpected := dw.sequencer.GetExpectedBlock()
	rpcHeightInt := rpcHeight.Int64()
	gapSize := rpcHeightInt - dbHeight
	isSpaceTimeTear := gapSize > dw.gapThreshold && sequencerExpected.Int64() < rpcHeightInt-dw.gapThreshold

	Logger.Info("🛡️ DeadlockWatchdog: State snapshot",
		slog.Int64("rpc_height", rpcHeightInt),
		slog.Int64("db_watermark", dbHeight),
		slog.String("sequencer_expected", sequencerExpected.String()),
		slog.Duration("idle_time", idleTime))

	if !isSpaceTimeTear {
		Logger.Debug("DeadlockWatchdog: Not a space-time tear, skipping",
			slog.Int64("gap_size", gapSize),
			slog.Bool("is_space_time_tear", isSpaceTimeTear))
		return nil
	}

	return &HealingState{
		IdleTime:          idleTime,
		RPCHeight:         rpcHeightInt,
		DBHeight:          dbHeight,
		SequencerExpected: sequencerExpected.Int64(),
		GapSize:           gapSize,
		IsSpaceTimeTear:   isSpaceTimeTear,
		NewCursorHeight:   rpcHeightInt - 1,
	}
}

// postHeal 自愈后处理（SSOT更新、Gap重调度）
func (dw *DeadlockWatchdog) postHeal(ctx context.Context, state *HealingState) {
	// 🔥 SSOT: 通过 Orchestrator 更新系统状态（单一控制面）
	orchestrator := GetOrchestrator()
	if orchestrator != nil {
		orchestrator.SetSystemState(SystemStateHealing)
	}

	// 🔧 Step 4/4: 重新调度 [dbHeight+1, rpcHeight] 范围的抓取任务。
	if dw.fetcher != nil && state.DBHeight < state.RPCHeight-1 {
		fetchFrom := new(big.Int).SetInt64(state.DBHeight + 1)
		fetchTo := new(big.Int).SetInt64(state.RPCHeight)
		Logger.Info("🔧 DeadlockWatchdog: Step 4/4: Rescheduling gap fetch",
			slog.Int64("from", fetchFrom.Int64()),
			slog.Int64("to", fetchTo.Int64()),
			slog.Int64("blocks", fetchTo.Int64()-fetchFrom.Int64()+1))
		go func() {
			if err := dw.fetcher.Schedule(ctx, fetchFrom, fetchTo); err != nil {
				Logger.Error("❌ DeadlockWatchdog: Gap reschedule failed",
					slog.String("error", err.Error()))
			}
		}()
	} else if dw.fetcher == nil {
		Logger.Warn("⚠️ DeadlockWatchdog: No fetcher wired — gap range not rescheduled. Call SetFetcher() after construction.")
	}
}
