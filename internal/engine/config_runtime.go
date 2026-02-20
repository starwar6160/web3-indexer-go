package engine

// config_runtime.go
// 配置与运行时状态分离（StaticConfig vs RuntimeParams）
//
// 设计原则：
//   - StaticConfig：启动时确定，不随运行改变（来自 IndexerConfig）
//   - RuntimeParams：运行时自适应调整（不可热更新，由引擎内部逻辑驱动）
//
// 这解决了 CoordinatorState 中 SafetyBuffer/SuccessCount 等字段
// 与真正的业务状态（LatestHeight/SyncedCursor 等）混杂的问题。

// RuntimeParams 运行时自适应参数（引擎内部自动调整，不对外暴露为配置）
// 这些参数从 IndexerConfig 初始化，但在运行时由引擎自适应修改
type RuntimeParams struct {
	// SafetyBuffer 动态安全缓冲（初始值来自配置，运行时自适应）
	// 当遇到 404 时增大，连续成功时减小
	// 初始值：1（Anvil）或 3（Testnet）
	SafetyBuffer uint64

	// SuccessCount 连续成功计数（用于触发 SafetyBuffer 缩减）
	// 达到 50 次连续成功后，SafetyBuffer 减 1
	SuccessCount uint64
}

// DefaultRuntimeParams 从 IndexerConfig 初始化运行时参数
func DefaultRuntimeParams(cfg IndexerConfig) RuntimeParams {
	buffer := uint64(1)
	if cfg.SyncMode == SyncModeBalanced || cfg.SyncMode == SyncModeEco {
		buffer = 3
	}
	return RuntimeParams{
		SafetyBuffer: buffer,
		SuccessCount: 0,
	}
}

// RuntimeSnapshot 运行时参数快照（用于 Debug API 展示）
type RuntimeSnapshot struct {
	SafetyBuffer uint64 `json:"safety_buffer"`
	SuccessCount uint64 `json:"success_count"`
}

// Snapshot 返回运行时参数的只读快照
func (r RuntimeParams) Snapshot() RuntimeSnapshot {
	return RuntimeSnapshot(r)
}

// ConfigAuditResult 配置审计结果（用于 /debug/config/audit 端点）
type ConfigAuditResult struct {
	// StaticConfig 当前静态配置（来自 ConfigManager）
	StaticConfig IndexerConfig `json:"static_config"`

	// RuntimeParams 当前运行时自适应参数
	RuntimeParams RuntimeSnapshot `json:"runtime_params"`

	// Warnings 配置一致性警告列表
	Warnings []string `json:"warnings"`

	// WarningCount 警告总数
	WarningCount int `json:"warning_count"`

	// IsConsistent 配置是否自洽
	IsConsistent bool `json:"is_consistent"`
}

// auditConfigConsistency 检查配置与运行时状态的一致性
// 返回所有发现的警告
func auditConfigConsistency(cfg IndexerConfig, orchSnap CoordinatorState) []string {
	var warnings []string

	// 警告 1：AlwaysActive=true 但系统处于 EcoMode
	if cfg.AlwaysActive && orchSnap.IsEcoMode {
		warnings = append(warnings, "CONFLICT: AlwaysActive=true but system is in EcoMode")
	}

	// 警告 2：SyncModeAggressive 但 MaxRPS 过低
	if cfg.SyncMode == SyncModeAggressive && cfg.MaxRPS < 10 {
		warnings = append(warnings, "SUBOPTIMAL: SyncMode=aggressive but MaxRPS<10, may cause throttling")
	}

	// 警告 3：DemoMode=true 在生产环境（非 Anvil）
	if cfg.DemoMode && cfg.MaxRPS < 100 {
		warnings = append(warnings, "RISK: DemoMode=true in non-local environment (MaxRPS<100)")
	}

	// 警告 4：FetcherConcurrency 过高导致 RPS 超限
	estimatedRPS := float64(cfg.FetcherConcurrency) * 2.0
	if estimatedRPS > cfg.MaxRPS*1.5 {
		warnings = append(warnings, "OVERLOAD_RISK: FetcherConcurrency may generate more RPS than MaxRPS allows")
	}

	// 警告 5：BatchSize 过大（超过 500 可能导致 RPC 超时）
	if cfg.BatchSize > 500 {
		warnings = append(warnings, "TIMEOUT_RISK: BatchSize>500 may cause RPC provider timeouts")
	}

	// 警告 6：RPCQuotaThreshold 与 RPCBalancedThreshold 差距过小
	if cfg.RPCQuotaThreshold-cfg.RPCBalancedThreshold < 0.1 {
		warnings = append(warnings, "NARROW_BAND: RPCQuotaThreshold and RPCBalancedThreshold are too close (<0.1)")
	}

	return warnings
}
