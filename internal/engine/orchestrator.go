package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
)

// 🎯 ZeroMQ 风格消息协议

// MsgType 消息类型枚举
type MsgType int

const (
	CmdUpdateChainHeight   MsgType = iota // 发现新块高度
	CmdCommitBatch                        // 成功同步了一批交易 (逻辑完成)
	CmdCommitDisk                         // 成功落盘 (物理完成) - 🔥 横滨实验室 SSOT 关键
	CmdResetCursor                        // 强制重置游标 (用于 Reorg)
	CmdIncrementTransfers                 // 增加转账计数
	CmdToggleEcoMode                      // 环境/配额触发休眠切换
	CmdSetSystemState                     // 设置系统状态
	CmdFetchFailed                        // 抓取失败 (用于调整安全缓冲)
	CmdFetchSuccess                       // 抓取成功 (用于重置失败计数)
	CmdNotifyFetched                      // 🚀 🔥 内存同步高度 (Fetcher 进度)
	CmdNotifyFetchProgress                // 🚀 🔥 新增：影子进度 (用于 UI 先行跳动)
	CmdLogEvent                           // 🚀 🔥 实时日志事件 (用于 UI 日志流)
	ReqGetStatus                          // UI 查询状态 (REQ/REP)
	ReqGetSnapshot                        // 获取状态快照 (REQ/REP)
)

// Message ZeroMQ 风格的消息结构
type Message struct {
	Type     MsgType
	Data     interface{}
	Reply    chan interface{} // 用于同步查询 (REQ/REP)
	Sequence uint64           // 全链路追踪 ID
}

// CoordinatorState 核心状态单例 (SSOT - Single Source of Truth)
// 所有状态的唯一真实来源,只有协调器能修改
type CoordinatorState struct {
	LatestHeight     uint64  // 链上最新高度
	TargetHeight     uint64  // 🎯 考虑安全垫后的目标高度
	FetchedHeight    uint64  // 🚀 🔥 新增：内存同步高度 (Fetcher 进度)
	SyncedCursor     uint64  // 数据库游标（已索引）
	Transfers        uint64  // 总转账数
	IsEcoMode        bool    // 是否处于休眠模式
	Progress         float64 // 同步进度百分比（统一计算，避免前端悖论）
	SystemState      SystemStateEnum
	UpdatedAt        time.Time              // 状态更新时间
	LastUserActivity time.Time              // 🔥 最后一次用户活动时间（用于休眠决策）
	SafetyBuffer     uint64                 // 🚀 动态安全缓冲 (解决追尾 404)
	SuccessCount     uint64                 // 🚀 🔥 新增：连续成功计数
	JobsDepth        int                    // 🔥 任务队列深度
	ResultsDepth     int                    // 🔥 结果队列深度
	LogEntry         map[string]interface{} // 🚀 🔥 新增：最新的日志条目
}

// Orchestrator 统一协调器（Actor 模型）
// 状态是私有的，只有协调器自己能改；外部只能通过发送"指令"来请求变更
type Orchestrator struct {
	cmdChan  chan Message     // 命令通道（深度缓冲应对 Anvil 高并发）
	state    CoordinatorState // 私有状态（仅协调器能改）
	mu       sync.RWMutex     // 仅用于对外提供快照读取
	snapshot CoordinatorState // 对外只读快照
	msgSeq   uint64           // 消息序列号生成器
	ctx      context.Context
	cancel   context.CancelFunc

	// 🔥 横滨实验室：环境感知配置
	isYokohamaLab bool // Anvil 环境 (128G RAM)

	// 🔥 订阅者管理（用于 WS 广播）
	broadcastCh chan CoordinatorState
	subscribers []chan CoordinatorState

	// 🔥 结构化日志配置
	enableProfiling bool

	// 🔥 消息合并策略（防止 Channel 溢出）
	pendingHeightUpdate *uint64 // 待合并的高度更新
	lastHeightMergeTime time.Time

	// 🔥 异步持久化流水线
	asyncWriter *AsyncWriter // 异步写入器引用

	// 🔥 时空连续性守卫（检测"位点倒挂"）
	linearityGuard *LinearityGuard
	fetcher        *Fetcher // 🚀 🔥 组件引用
	strategy       Strategy // 🚀 🔥 新增：运行策略 (Anvil vs Testnet)

	// 运行时自适应参数（与静态 IndexerConfig 分离）
	runtimeParams RuntimeParams
}

var (
	orchestrator     *Orchestrator
	orchestratorOnce sync.Once
)

// GetOrchestrator 返回协调器单例
func GetOrchestrator() *Orchestrator {
	orchestratorOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		rp := DefaultRuntimeParams(DefaultConfig())
		orchestrator = &Orchestrator{
			cmdChan: make(chan Message, 50000), // 🚀 16G RAM 调优：适中缓冲区
			state: CoordinatorState{
				UpdatedAt:        time.Now(),
				SystemState:      SystemStateUnknown,
				LastUserActivity: time.Now(),      // 初始化为当前时间
				SafetyBuffer:     rp.SafetyBuffer, // 来自 RuntimeParams
			},
			runtimeParams:       rp,
			subscribers:         make([]chan CoordinatorState, 0, 8),
			ctx:                 ctx,
			cancel:              cancel,
			enableProfiling:     true,
			isYokohamaLab:       false, // 稍后在 Init() 中检测
			pendingHeightUpdate: nil,
			lastHeightMergeTime: time.Now(),
		}
		// 🔥 在 orchestrator 创建后再初始化 LinearityGuard
		orchestrator.linearityGuard = NewLinearityGuard(orchestrator)
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

	slog.Info("🎼 Orchestrator initialized", "strategy", strategy.Name())
}

// LoadInitialState 从数据库加载初始状态（通过 Repository 接口，不直接操作 SQL）
func (o *Orchestrator) LoadInitialState(repo IndexerRepository, chainID int64) error {
	cursor, err := repo.GetSyncCursor(context.Background(), chainID)
	if err != nil {
		slog.Warn("🎼 Orchestrator: GetSyncCursor failed, starting from 0", "err", err)
		return nil
	}
	if cursor > 0 {
		o.mu.Lock()
		o.state.SyncedCursor = uint64(cursor) //nolint:gosec // cursor is a valid block height
		o.snapshot = o.state
		o.mu.Unlock()
		slog.Info("🎼 Orchestrator: Initial state aligned from DB", "cursor", cursor)
	}
	return nil
}

// LoadInitialStateFromDB 兼容方法：接受 *sqlx.DB（供现有调用方使用，内部构造 adapter）
func (o *Orchestrator) LoadInitialStateFromDB(db *sqlx.DB, chainID int64) error {
	// 延迟导入避免循环依赖：通过直接 SQL 实现（保留原始逻辑作为 fallback）
	var lastSyncedBlock string
	err := db.GetContext(context.Background(), &lastSyncedBlock,
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
	if err != nil || lastSyncedBlock == "" || lastSyncedBlock == "0" {
		var maxInDB int64
		if err2 := db.GetContext(context.Background(), &maxInDB, "SELECT COALESCE(MAX(number), 0) FROM blocks"); err2 == nil && maxInDB > 0 {
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

// 🔥 核心：调度循环（单一入口点）
// 所有的状态变更都发生在这个协程里，确保逻辑绝对线性
func (o *Orchestrator) loop() {
	slog.Info("🎼 Coordinator: SSOT Engine Online", "location", "Yokohama-Lab-Primary")

	// 🔥 自动化休眠决策引擎：每 5 秒进行一次"自我审查"
	decisionTicker := time.NewTicker(5 * time.Second)
	defer decisionTicker.Stop()

	// 🔥 消息合并定时器：每 100ms 合并一次高度更新（防止 Anvil 高频推送溢出）
	mergeTicker := time.NewTicker(100 * time.Millisecond)
	defer mergeTicker.Stop()

	// 📊 遥测定时器：每 1 秒输出一行 AI 专用诊断日志
	telemetryTicker := time.NewTicker(1 * time.Second)
	defer telemetryTicker.Stop()

	// 🚀 现实审计定时器：每 30 秒检查一次"未来人"状态
	realityAuditTicker := time.NewTicker(30 * time.Second)
	defer realityAuditTicker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			slog.Warn("🎼 Coordinator: Shutting down...")
			return

		case msg := <-o.cmdChan:
			o.process(msg)

		case <-decisionTicker.C:
			o.evaluateEcoMode()
			o.evaluateSystemState()

		case <-mergeTicker.C:
			o.flushPendingHeightUpdate()

		case <-telemetryTicker.C:
			o.LogPulse(o.ctx)

		// 🚀 Runtime reality audit
		case <-realityAuditTicker.C:
			o.auditReality(o.ctx)
		}
	}
}

// process 处理消息（状态机核心逻辑）
func (o *Orchestrator) process(msg Message) {
	start := time.Now()
	o.handleMessage(msg)
	o.updateDerivedMetrics()
	o.broadcastUpdate()
	o.profileSlowProcessing(start, msg)
}

func (o *Orchestrator) updateDerivedMetrics() {
	if o.state.LatestHeight > 0 {
		latest := float64(o.state.LatestHeight)
		synced := float64(o.state.SyncedCursor)
		o.state.Progress = (synced / latest) * 100
		if o.state.Progress > 100.0 {
			o.state.Progress = 100.0
		}
	}
	o.state.UpdatedAt = time.Now()
}

func (o *Orchestrator) broadcastUpdate() {
	o.mu.Lock()
	o.snapshot = o.state
	o.mu.Unlock()
	select {
	case o.broadcastCh <- o.snapshot:
	default:
		slog.Debug("🎼 Broadcast channel full, skipping")
	}
}

func (o *Orchestrator) profileSlowProcessing(start time.Time, msg Message) {
	if !o.enableProfiling {
		return
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		slog.Warn("🎼 Slow Process", "seq", msg.Sequence, "dur", elapsed, "type", msg.Type)
	}
}

// 🔥 独立的广播协程：解耦 WS 推送和业务逻辑
// 无论后台是在疯狂同步还是进入 Eco-Mode，这个广播器都是独立的
func (o *Orchestrator) broadcaster() {
	// 节流：每 500ms 推送一次快照（避免高频推送）
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastSnapshot CoordinatorState

	for {
		select {
		case <-o.ctx.Done():
			return

		case snapshot := <-o.broadcastCh:
			// 收到新快照，记录但不立即推送
			lastSnapshot = snapshot

		case <-ticker.C:
			// 定时推送最新的快照
			if len(o.subscribers) > 0 {
				o.broadcastSnapshot(lastSnapshot)
			}
		}
	}
}

// broadcastSnapshot 广播快照到所有订阅者
func (o *Orchestrator) broadcastSnapshot(snapshot CoordinatorState) {
	for _, ch := range o.subscribers {
		select {
		case ch <- snapshot:
			// 成功发送
		default:
			// 订阅者慢，跳过
			slog.Debug("🎼 Subscriber slow, skipping")
		}
	}
}

// 🔥 对外接口 (API Entry)

// Dispatch 发送异步命令（非阻塞）
// 用于 CmdUpdateChainHeight, CmdCommitBatch, CmdToggleEcoMode 等异步命令
func (o *Orchestrator) Dispatch(t MsgType, data interface{}) uint64 {
	seq := atomic.AddUint64(&o.msgSeq, 1)
	msg := Message{Type: t, Data: data, Sequence: seq}

	select {
	case o.cmdChan <- msg:
		// 成功入队
		return seq
	default:
		slog.Error("🎼 Backpressure: Command channel full!", "seq", seq, "type", t)
		return seq
	}
}

// DispatchSync 发送同步查询（阻塞）
// 用于 ReqGetStatus, ReqGetSnapshot 等需要立即响应的查询
func (o *Orchestrator) DispatchSync(t MsgType, data interface{}) (interface{}, error) {
	seq := atomic.AddUint64(&o.msgSeq, 1)
	replyCh := make(chan interface{}, 1)
	msg := Message{Type: t, Data: data, Reply: replyCh, Sequence: seq}

	select {
	case o.cmdChan <- msg:
		// 成功入队
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("dispatch timeout: seq=%d", seq)
	}

	// 等待响应
	select {
	case result := <-replyCh:
		return result, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("reply timeout: seq=%d", seq)
	}
}

// GetSnapshot 获取状态快照（极速、无阻塞）
// 用于 API 查询，直接返回内存快照，不经过消息队列
func (o *Orchestrator) GetSnapshot() CoordinatorState {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.snapshot
}

// GetRuntimeParams 返回运行时自适应参数快照（线程安全）
func (o *Orchestrator) GetRuntimeParams() RuntimeSnapshot {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.runtimeParams.Snapshot()
}

// Subscribe 订阅状态快照（用于 WebSocket 推送）
// 返回一个只读 channel，定期接收最新快照
func (o *Orchestrator) Subscribe() <-chan CoordinatorState {
	ch := make(chan CoordinatorState, 100) // 缓冲 100 个快照
	o.subscribers = append(o.subscribers, ch)
	slog.Info("🎼 New subscriber registered", "total", len(o.subscribers))
	return ch
}

// 🔥 兼容性方法（用于现有代码迁移）

// UpdateChainHead 更新链头高度（兼容方法）
func (o *Orchestrator) UpdateChainHead(height uint64) {
	// � 首先检查时空连续性（防止位点倒挂）
	if o.linearityGuard != nil {
		o.linearityGuard.CheckLinearity(height)
	}

	// �� 🔥 资深调优：不再走 cmdChan 异步队列，而是立即通过锁更新 state 和 snapshot
	// 这解决了 UI 上 'Latest: 0' 滞后的问题，确保链脉搏瞬时响应
	o.mu.Lock()
	if height > o.state.LatestHeight {
		o.state.LatestHeight = height

		// 🚀 计算目标高度 (Latest - SafetyBuffer)
		if height > o.state.SafetyBuffer {
			o.state.TargetHeight = height - o.state.SafetyBuffer
		} else {
			o.state.TargetHeight = 0
		}

		// 🚀 物理对齐：立即更新 snapshot，让 GetUIStatus 拿到的总是最新值
		o.snapshot = o.state
		o.state.UpdatedAt = time.Now()
	}
	o.mu.Unlock()

	// 仍然发送一个轻量级消息以触发 loop 里的 evaluate 逻辑（可选）
}

// AdvanceCursor 前进数据库游标（兼容方法）
func (o *Orchestrator) AdvanceCursor(cursor uint64) {
	o.Dispatch(CmdCommitBatch, cursor)
}

// AdvanceDBCursor 前进数据库游标（物理同步）
func (o *Orchestrator) AdvanceDBCursor(height uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if height > o.state.SyncedCursor {
		o.state.SyncedCursor = height
		slog.Info("🎼 Orchestrator: Synced cursor advanced", "height", height)
	}
}

// IncrementTransfers 增加转账计数（兼容方法）
func (o *Orchestrator) IncrementTransfers(count uint64) {
	o.Dispatch(CmdIncrementTransfers, count)
}

// SetEcoMode 设置休眠模式（兼容方法）
func (o *Orchestrator) SetEcoMode(enabled bool) {
	o.Dispatch(CmdToggleEcoMode, enabled)
}

// SetSystemState 设置系统状态（兼容方法）
func (o *Orchestrator) SetSystemState(state SystemStateEnum) {
	o.Dispatch(CmdSetSystemState, state)
}

// 🔥 辅助方法

// GetSyncLag 获取同步滞后（块数）
func (o *Orchestrator) GetSyncLag() int64 {
	snap := o.GetSnapshot()
	if snap.LatestHeight == 0 {
		return 0
	}
	latest := snap.LatestHeight
	if latest > math.MaxInt64 {
		latest = math.MaxInt64
	}
	synced := snap.SyncedCursor
	if synced > math.MaxInt64 {
		synced = math.MaxInt64
	}
	lag := int64(latest) - int64(synced) // #nosec G115 - values clamped to MaxInt64 above
	if lag < 0 {
		return 0 // 时间旅行场景
	}
	return lag
}

// GetProgress 获取同步进度百分比
func (o *Orchestrator) GetProgress() float64 {
	snap := o.GetSnapshot()
	return snap.Progress
}

// IsEcoMode 检查是否处于休眠模式
func (o *Orchestrator) IsEcoMode() bool {
	snap := o.GetSnapshot()
	return snap.IsEcoMode
}

// ProfilingLog 性能日志记录（用于调试）
func (o *Orchestrator) ProfilingLog(stage string, duration time.Duration) {
	if !o.enableProfiling {
		return
	}

	if duration > 100*time.Millisecond {
		slog.Warn("📊 [Profiler] Slow operation detected",
			"stage", stage,
			"duration_ms", duration.Milliseconds())
	}
}

// DumpSystemState 系统状态转储（用于 Watchdog 触发时）
func (o *Orchestrator) DumpSystemState() map[string]interface{} {
	snap := o.GetSnapshot()

	return map[string]interface{}{
		"latest_height": snap.LatestHeight,
		"synced_cursor": snap.SyncedCursor,
		"transfers":     snap.Transfers,
		"sync_lag":      o.GetSyncLag(),
		"progress":      snap.Progress,
		"is_eco_mode":   snap.IsEcoMode,
		"system_state":  snap.SystemState.String(),
		"updated_at":    snap.UpdatedAt.Format(time.RFC3339),
	}
}

// GetStatus 返回一个全面的 API 响应 Map

func (o *Orchestrator) GetStatus(_ context.Context, _ *sqlx.DB, rpcPool RPCClient, version string) map[string]interface{} {

	snap := o.GetSnapshot()

	// 🚀 G115 安全计算

	syncLag := SafeInt64Diff(snap.LatestHeight, snap.SyncedCursor)

	if syncLag < 0 {

		syncLag = 0

	}

	fetchProgress := 0.0

	if snap.LatestHeight > 0 {

		fetchProgress = float64(snap.FetchedHeight) / float64(snap.LatestHeight) * 100

		if fetchProgress > 100.0 {

			fetchProgress = 100.0

		}

	}

	status := map[string]interface{}{

		"version": version,

		"state": snap.SystemState.String(),

		"latest_block": fmt.Sprintf("%d", snap.LatestHeight),

		"target_height": fmt.Sprintf("%d", snap.TargetHeight),

		"latest_fetched": fmt.Sprintf("%d", snap.FetchedHeight), // 🚀 内存扫描进度

		"fetch_progress": fetchProgress,

		"safety_buffer": snap.SafetyBuffer,

		"latest_indexed": fmt.Sprintf("%d", snap.SyncedCursor),

		"sync_lag": syncLag,

		"transfers": snap.Transfers,

		"is_eco_mode": snap.IsEcoMode,

		"progress": snap.Progress,

		"updated_at": snap.UpdatedAt.Format(time.RFC3339),

		"is_healthy": rpcPool.GetHealthyNodeCount() > 0,

		"rpc_nodes": map[string]int{

			"healthy": rpcPool.GetHealthyNodeCount(),

			"total": rpcPool.GetTotalNodeCount(),
		},

		"jobs_depth": snap.JobsDepth,

		"results_depth": snap.ResultsDepth,

		"jobs_capacity": 160, // 💡 5600U 专供

		"results_capacity": 15000,

		"tps": GetMetrics().GetWindowTPS(),

		"bps": GetMetrics().GetWindowBPS(),
	}

	// 注入 AsyncWriter 指标

	if o.asyncWriter != nil {

		writerMetrics := o.asyncWriter.GetMetrics()

		for k, v := range writerMetrics {

			status["writer_"+k] = v

		}

	}

	return status

}

// Shutdown 优雅关闭协调器
func (o *Orchestrator) Shutdown() {
	slog.Info("🎼 Orchestrator shutting down...")
	o.cancel()
	close(o.cmdChan)
	close(o.broadcastCh)

	// 关闭异步写入器
	if o.asyncWriter != nil {
		if err := o.asyncWriter.Shutdown(30 * time.Second); err != nil {
			slog.Error("🎼 AsyncWriter shutdown failed", "err", err)
		}
	}
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
	o.state = CoordinatorState{
		UpdatedAt:        time.Now(),
		SystemState:      SystemStateUnknown,
		LastUserActivity: time.Now(),
		SafetyBuffer:     1,
	}
	o.snapshot = o.state
	slog.Info("🎼 Orchestrator: State reset for testing")
}

// 🔥 自动化系统状态评估
func (o *Orchestrator) evaluateSystemState() {
	// 🚀 更新队列深度快照
	jobsDepth := 0
	resultsDepth := 0
	if o.fetcher != nil {
		jobsDepth = o.fetcher.QueueDepth()
		resultsDepth = o.fetcher.ResultsDepth()
		o.state.JobsDepth = jobsDepth
		o.state.ResultsDepth = resultsDepth
	}

	// 🚀 🔥 同步到 GlobalState 以供 UIProjection 和其他组件使用
	// 注意：此处我们需要获取 Sequencer 的深度，但 Orchestrator 暂时没存，先填 0
	if jobsDepth > math.MaxInt32 {
		jobsDepth = math.MaxInt32
	}
	if resultsDepth > math.MaxInt32 {
		resultsDepth = math.MaxInt32
	}
	GetGlobalState().UpdatePipelineDepth(int32(jobsDepth), int32(resultsDepth), 0) // #nosec G115 - values clamped to MaxInt32 above

	snap := GetGlobalState().Snapshot()

	// 1. 背压检查
	if snap.ResultsDepth > snap.PipelineDepth*80/100 {
		o.state.SystemState = SystemStateThrottled
		return
	}

	// 如果安全缓冲开启，说明正在优化追尾
	if o.state.SafetyBuffer > 1 {
		o.state.SystemState = SystemStateOptimizing
		return
	}

	// 默认状态
	if o.state.SystemState == SystemStateOptimizing || o.state.SystemState == SystemStateThrottled || o.state.SystemState == SystemStateUnknown {
		o.state.SystemState = SystemStateRunning
	}
}

// 🔥 自动化休眠决策引擎（Eco-Mode Decision Engine）
// 每 5 秒执行一次"自我审查"，根据同步进度、用户活跃度和配额自动切换模式
func (o *Orchestrator) evaluateEcoMode() {
	// 读取当前状态（无需加锁，因为在同一个协程中）
	lag := o.state.LatestHeight - o.state.SyncedCursor
	idleTime := time.Since(o.state.LastUserActivity)

	// --- 核心决策树 ---

	reason := ""
	shouldBeEco := false

	// 1. 🔥 如果还在追赶高度，严禁休眠（解决之前的 40973 缺失却休眠的问题）
	if lag > 10 {
		shouldBeEco = false
		reason = "Syncing blocks"
	} else if idleTime < 2*time.Minute {
		// 2. 如果近期有用户操作，保持活跃
		shouldBeEco = false
		reason = "User active"
	} else {
		// 3. 只有既没任务、又没人看时，才休眠
		shouldBeEco = true
		reason = "Idle and synced"
	}

	// --- 执行变更 ---

	if o.state.IsEcoMode != shouldBeEco {
		o.state.IsEcoMode = shouldBeEco
		slog.Warn("🎼 DecisionEngine: Mode Switch",
			"to_eco", shouldBeEco,
			"reason", reason,
			"lag", lag,
			"idle_sec", int(idleTime.Seconds()))

		// 此处通过单例入口，统一通知外部组件（如暂停 Fetcher 或 更新 UI）
		// 注意：不直接调用 fetcher.Pause()，而是发送消息给 LazyManager
		if shouldBeEco {
			// 通知 LazyManager 进入休眠
			// TODO: 通过事件系统通知 LazyManager
			_ = 1
		} else {
			// 通知 LazyManager 唤醒
			// TODO: 通过事件系统通知 LazyManager
			_ = 0
		}
	}
}

// 🔥 消息合并：刷新待处理的高度更新（防止 Channel 溢出）
func (o *Orchestrator) flushPendingHeightUpdate() {
	if o.pendingHeightUpdate != nil {
		h := *o.pendingHeightUpdate
		if h > o.state.LatestHeight {
			o.state.LatestHeight = h
			slog.Debug("🎼 Height update applied", "val", h)
		}
		o.pendingHeightUpdate = nil
	}
}

// 🔥 记录用户活动（由 LazyManager.Trigger() 调用）
func (o *Orchestrator) RecordUserActivity() {
	o.state.LastUserActivity = time.Now()
	slog.Debug("🎼 User activity recorded")
}

// DispatchLog 发送实时日志到 UI
func (o *Orchestrator) DispatchLog(level string, message string, args ...interface{}) {
	data := map[string]interface{}{
		"level":   level,
		"msg":     message,
		"ts":      time.Now().Unix(),
		"details": args,
	}
	o.Dispatch(CmdLogEvent, data)
}

// auditReality performs runtime reality audit to detect "future human" state
func (o *Orchestrator) auditReality(ctx context.Context) {
	// Skip if no fetcher/RPC pool available
	if o.fetcher == nil || o.fetcher.pool == nil {
		return
	}

	// Get actual RPC height
	rpcHeightBig, err := o.fetcher.pool.GetLatestBlockNumber(ctx)
	if err != nil {
		slog.Debug("🎼 RealityAudit: RPC query failed", "err", err)
		return
	}
	rpcHeight := rpcHeightBig.Uint64()

	// Get current memory state
	snap := o.GetSnapshot()
	tolerance := uint64(1000) // Configurable tolerance

	// Check for "Future Human" state
	isInFuture := false
	reason := ""

	if snap.FetchedHeight > rpcHeight+tolerance {
		isInFuture = true
		reason = "fetched_height_exceeds_rpc"
	} else if snap.LatestHeight > rpcHeight+tolerance {
		isInFuture = true
		reason = "latest_height_exceeds_rpc"
	} else if snap.SyncedCursor > rpcHeight+tolerance {
		isInFuture = true
		reason = "synced_cursor_exceeds_rpc"
	}

	if isInFuture {
		slog.Error("🚨 REALITY_AUDIT_FAILURE: Future Human detected!",
			"reason", reason,
			"rpc_actual", rpcHeight,
			"mem_latest", snap.LatestHeight,
			"mem_fetched", snap.FetchedHeight,
			"mem_synced", snap.SyncedCursor,
			"tolerance", tolerance,
			"action", "triggering_snap_to_reality")

		// Trigger automatic collapse
		o.SnapToReality(rpcHeight)

		// Update system state
		o.Dispatch(CmdSetSystemState, SystemStateHealing)
		o.DispatchLog("ERROR", "Reality collapse triggered - system realigning to RPC truth")
	}
}
