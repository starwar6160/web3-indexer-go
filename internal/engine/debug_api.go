package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"time"
)

// 重复字符串常量（goconst 要求）
const (
	healthStatusOK       = "ok"
	healthStatusWarn     = "warn"
	healthStatusError    = "error"
	healthStatusDegraded = "degraded"
	healthStatusCritical = "critical"
)

// GlobalStateProvider 抽象 GlobalState 单例的只读接口（用于测试注入）
type GlobalStateProvider interface {
	Snapshot() Snapshot
}

// HeightOracleProvider 抽象 HeightOracle 单例的只读接口（用于测试注入）
type HeightOracleProvider interface {
	ChainHead() int64
	IndexedHead() int64
	SyncLag() int64
	UpdatedAt() time.Time
	Snapshot() HeightSnapshot
}

// DebugServer 调试 API 服务器（Introspection API）
type DebugServer struct {
	orchestrator   *Orchestrator
	configManager  *ConfigManager
	gsProvider     GlobalStateProvider  // nil => 使用全局单例 GetGlobalState()
	oracleProvider HeightOracleProvider // nil => 使用全局单例 GetHeightOracle()
}

// NewDebugServer 创建调试服务器（使用全局单例）
func NewDebugServer(o *Orchestrator, cm *ConfigManager) *DebugServer {
	return &DebugServer{orchestrator: o, configManager: cm}
}

// NewDebugServerWithProviders 创建调试服务器（显式依赖注入，用于测试）
func NewDebugServerWithProviders(o *Orchestrator, cm *ConfigManager, gs GlobalStateProvider, oracle HeightOracleProvider) *DebugServer {
	return &DebugServer{orchestrator: o, configManager: cm, gsProvider: gs, oracleProvider: oracle}
}

// globalState 返回注入的 provider 或全局单例
func (d *DebugServer) globalState() GlobalStateProvider {
	if d.gsProvider != nil {
		return d.gsProvider
	}
	return GetGlobalState()
}

// heightOracle 返回注入的 provider 或全局单例
func (d *DebugServer) heightOracle() HeightOracleProvider {
	if d.oracleProvider != nil {
		return d.oracleProvider
	}
	return GetHeightOracle()
}

// RegisterDebugRoutes 注册所有调试端点
func (d *DebugServer) RegisterDebugRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/debug/snapshot", d.handleFullSnapshot)
	mux.HandleFunc("/debug/health/components", d.handleComponentHealth)
	mux.HandleFunc("/debug/pipeline/trace", d.handlePipelineTrace)
	mux.HandleFunc("/debug/config/audit", d.handleConfigAudit)
	mux.HandleFunc("/debug/race/check", d.handleRaceCheck)
	mux.HandleFunc("/debug/goroutines/dump", d.handleGoroutineDump)
	mux.HandleFunc("/debug/goroutines/snapshot", d.handleGoroutineSnapshot)
}

// ─── 数据结构 ─────────────────────────────────────────────────────────────────

// DebugSnapshot 全量状态快照
type DebugSnapshot struct {
	Timestamp    int64                 `json:"timestamp_ms"`
	Orchestrator OrchestratorDebugInfo `json:"orchestrator"`
	GlobalState  GlobalStateDebugInfo  `json:"global_state"`
	HeightOracle HeightOracleDebugInfo `json:"height_oracle"`
	Pipeline     PipelineDebugInfo     `json:"pipeline"`
	Config       ConfigDebugInfo       `json:"config"`
	Runtime      RuntimeDebugInfo      `json:"runtime"`
	Consistency  ConsistencyReport     `json:"consistency"`
}

// OrchestratorDebugInfo Orchestrator 内部状态
type OrchestratorDebugInfo struct {
	LatestHeight  uint64  `json:"latest_height"`
	TargetHeight  uint64  `json:"target_height"`
	FetchedHeight uint64  `json:"fetched_height"`
	SyncedCursor  uint64  `json:"synced_cursor"`
	Transfers     uint64  `json:"transfers"`
	IsEcoMode     bool    `json:"is_eco_mode"`
	Progress      float64 `json:"progress"`
	SystemState   string  `json:"system_state"`
	SafetyBuffer  uint64  `json:"safety_buffer"`
	JobsDepth     int     `json:"jobs_depth"`
	ResultsDepth  int     `json:"results_depth"`
	CmdChanLen    int     `json:"cmd_chan_len"`
	CmdChanCap    int     `json:"cmd_chan_cap"`
	SubscriberCnt int     `json:"subscriber_count"`
	UpdatedAt     string  `json:"updated_at"`
}

// GlobalStateDebugInfo GlobalState 内部状态
type GlobalStateDebugInfo struct {
	OnChainHeight  uint64  `json:"on_chain_height"`
	DBCursor       uint64  `json:"db_cursor"`
	TotalTransfers uint64  `json:"total_transfers"`
	SystemState    string  `json:"system_state"`
	PipelineDepth  int32   `json:"pipeline_depth"`
	QuotaUsage     float64 `json:"quota_usage"`
	JobsQueueDepth int32   `json:"jobs_queue_depth"`
	ResultsDepth   int32   `json:"results_depth"`
	LastUpdate     string  `json:"last_update"`
}

// HeightOracleDebugInfo HeightOracle 内部状态
type HeightOracleDebugInfo struct {
	ChainHead   int64  `json:"chain_head"`
	IndexedHead int64  `json:"indexed_head"`
	SyncLag     int64  `json:"sync_lag"`
	UpdatedAt   string `json:"updated_at"`
	StalenessMs int64  `json:"staleness_ms"`
}

// PipelineDebugInfo 数据流水线状态
type PipelineDebugInfo struct {
	CmdChanLen        int    `json:"cmd_chan_len"`
	CmdChanCap        int    `json:"cmd_chan_cap"`
	CmdChanUsagePct   int    `json:"cmd_chan_usage_pct"`
	BackpressureLevel string `json:"backpressure_level"`
}

// ConfigDebugInfo 当前配置快照
type ConfigDebugInfo struct {
	SyncMode           string  `json:"sync_mode"`
	MaxRPS             float64 `json:"max_rps"`
	FetcherConcurrency int     `json:"fetcher_concurrency"`
	BatchSize          int     `json:"batch_size"`
	DemoMode           bool    `json:"demo_mode"`
	AlwaysActive       bool    `json:"always_active"`
	RPCQuotaThreshold  float64 `json:"rpc_quota_threshold"`
}

// RuntimeDebugInfo Go 运行时信息
type RuntimeDebugInfo struct {
	NumGoroutines int    `json:"num_goroutines"`
	HeapAllocMB   uint64 `json:"heap_alloc_mb"`
	GOMAXPROCS    int    `json:"gomaxprocs"`
}

// ConsistencyReport 自动一致性检查（核心：发现 80% 问题）
type ConsistencyReport struct {
	IsHealthy                     bool     `json:"is_healthy"`
	HeightParadox                 bool     `json:"height_paradox"`
	HeightParadoxDesc             string   `json:"height_paradox_desc,omitempty"`
	MemoryDBGap                   int64    `json:"memory_db_gap"`
	MemoryDBGapWarn               bool     `json:"memory_db_gap_warn"`
	MemoryDBGapDesc               string   `json:"memory_db_gap_desc,omitempty"`
	WatermarkInversion            bool     `json:"watermark_inversion"`
	WatermarkInversionDesc        string   `json:"watermark_inversion_desc,omitempty"`
	CmdChanPressure               bool     `json:"cmd_chan_pressure"`
	CmdChanPressureDesc           string   `json:"cmd_chan_pressure_desc,omitempty"`
	OracleOrchDivergence          bool     `json:"oracle_orch_divergence"`
	OracleOrchDivergenceDesc      string   `json:"oracle_orch_divergence_desc,omitempty"`
	GlobalStateOrchDivergence     bool     `json:"global_state_orch_divergence"`
	GlobalStateOrchDivergenceDesc string   `json:"global_state_orch_divergence_desc,omitempty"`
	IssueCount                    int      `json:"issue_count"`
	Issues                        []string `json:"issues,omitempty"`
}

// ComponentHealth 单个组件健康状态
type ComponentHealth struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	CheckedAt string `json:"checked_at"`
}

// ComponentHealthReport 所有组件健康报告
type ComponentHealthReport struct {
	Overall    string            `json:"overall"`
	Components []ComponentHealth `json:"components"`
	CheckedAt  string            `json:"checked_at"`
}

// ─── HTTP 处理器 ──────────────────────────────────────────────────────────────

func (d *DebugServer) handleFullSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, d.BuildFullSnapshot(r.Context()))
}

func (d *DebugServer) handleComponentHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, d.BuildComponentHealth())
}

func (d *DebugServer) handlePipelineTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, d.BuildPipelineTrace())
}

func (d *DebugServer) handleConfigAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, d.BuildConfigAudit())
}

func (d *DebugServer) handleRaceCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, d.BuildRaceCheck())
}

// ─── 核心构建逻辑（可直接被集成测试调用，无需 HTTP）────────────────────────────

// BuildFullSnapshot 构建完整快照（供集成测试直接调用）
func (d *DebugServer) BuildFullSnapshot(_ context.Context) DebugSnapshot {
	o := d.orchestrator
	orchSnap := o.GetSnapshot()
	gsSnap := d.globalState().Snapshot()
	oracle := d.heightOracle()

	orchInfo := OrchestratorDebugInfo{
		LatestHeight:  orchSnap.LatestHeight,
		TargetHeight:  orchSnap.TargetHeight,
		FetchedHeight: orchSnap.FetchedHeight,
		SyncedCursor:  orchSnap.SyncedCursor,
		Transfers:     orchSnap.Transfers,
		IsEcoMode:     orchSnap.IsEcoMode,
		Progress:      orchSnap.Progress,
		SystemState:   orchSnap.SystemState.String(),
		SafetyBuffer:  orchSnap.SafetyBuffer,
		JobsDepth:     orchSnap.JobsDepth,
		ResultsDepth:  orchSnap.ResultsDepth,
		CmdChanLen:    len(o.cmdChan),
		CmdChanCap:    cap(o.cmdChan),
		SubscriberCnt: len(o.subscribers),
		UpdatedAt:     orchSnap.UpdatedAt.Format(time.RFC3339),
	}

	gsInfo := GlobalStateDebugInfo{
		OnChainHeight:  gsSnap.OnChainHeight,
		DBCursor:       gsSnap.DBCursor,
		TotalTransfers: gsSnap.TotalTransfers,
		SystemState:    gsSnap.SystemState.String(),
		PipelineDepth:  gsSnap.PipelineDepth,
		QuotaUsage:     gsSnap.QuotaUsage,
		JobsQueueDepth: gsSnap.JobsQueueDepth,
		ResultsDepth:   gsSnap.ResultsDepth,
		LastUpdate:     gsSnap.LastUpdate.Format(time.RFC3339),
	}

	oracleInfo := HeightOracleDebugInfo{
		ChainHead:   oracle.ChainHead(),
		IndexedHead: oracle.IndexedHead(),
		SyncLag:     oracle.SyncLag(),
		UpdatedAt:   oracle.UpdatedAt().Format(time.RFC3339),
		StalenessMs: time.Since(oracle.UpdatedAt()).Milliseconds(),
	}

	cmdLen := len(o.cmdChan)
	cmdCap := cap(o.cmdChan)
	usagePct := 0
	if cmdCap > 0 {
		usagePct = cmdLen * 100 / cmdCap
	}
	bpLevel := "none"
	switch {
	case usagePct > 90:
		bpLevel = "critical"
	case usagePct > 70:
		bpLevel = "high"
	case usagePct > 50:
		bpLevel = "medium"
	case usagePct > 20:
		bpLevel = "low"
	}

	pipelineInfo := PipelineDebugInfo{
		CmdChanLen:        cmdLen,
		CmdChanCap:        cmdCap,
		CmdChanUsagePct:   usagePct,
		BackpressureLevel: bpLevel,
	}

	configInfo := ConfigDebugInfo{}
	if d.configManager != nil {
		cfg := d.configManager.Get()
		configInfo = ConfigDebugInfo{
			SyncMode:           string(cfg.SyncMode),
			MaxRPS:             cfg.MaxRPS,
			FetcherConcurrency: cfg.FetcherConcurrency,
			BatchSize:          cfg.BatchSize,
			DemoMode:           cfg.DemoMode,
			AlwaysActive:       cfg.AlwaysActive,
			RPCQuotaThreshold:  cfg.RPCQuotaThreshold,
		}
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	runtimeInfo := RuntimeDebugInfo{
		NumGoroutines: runtime.NumGoroutine(),
		HeapAllocMB:   ms.HeapAlloc / 1024 / 1024,
		GOMAXPROCS:    runtime.GOMAXPROCS(0),
	}

	consistency := d.runConsistencyChecks(orchSnap, gsSnap, oracle)

	return DebugSnapshot{
		Timestamp:    time.Now().UnixMilli(),
		Orchestrator: orchInfo,
		GlobalState:  gsInfo,
		HeightOracle: oracleInfo,
		Pipeline:     pipelineInfo,
		Config:       configInfo,
		Runtime:      runtimeInfo,
		Consistency:  consistency,
	}
}

// runConsistencyChecks 执行所有一致性检查（复杂度已拆分到各子方法）
func (d *DebugServer) runConsistencyChecks(
	orchSnap CoordinatorState,
	gsSnap Snapshot,
	oracle HeightOracleProvider,
) ConsistencyReport {
	report := ConsistencyReport{IsHealthy: true}
	var issues []string

	issues = d.checkHeightParadox(orchSnap, &report, issues)
	issues = d.checkMemoryDBGap(orchSnap, &report, issues)
	issues = d.checkWatermarkInversion(orchSnap, &report, issues)
	issues = d.checkCmdChanPressure(&report, issues)
	issues = d.checkOracleDivergence(orchSnap, oracle, &report, issues)
	issues = d.checkGlobalStateDivergence(orchSnap, gsSnap, &report, issues)

	report.IssueCount = len(issues)
	report.Issues = issues
	if len(issues) > 0 {
		report.IsHealthy = false
	}
	return report
}

// checkHeightParadox 检查 SyncedCursor > LatestHeight 悖论
func (d *DebugServer) checkHeightParadox(snap CoordinatorState, r *ConsistencyReport, issues []string) []string {
	if snap.LatestHeight > 0 && snap.SyncedCursor > snap.LatestHeight {
		r.HeightParadox = true
		r.HeightParadoxDesc = fmt.Sprintf("SyncedCursor(%d) > LatestHeight(%d) by %d blocks",
			snap.SyncedCursor, snap.LatestHeight, snap.SyncedCursor-snap.LatestHeight)
		issues = append(issues, "HEIGHT_PARADOX: "+r.HeightParadoxDesc)
	}
	return issues
}

// checkMemoryDBGap 检查内存与磁盘差距过大
func (d *DebugServer) checkMemoryDBGap(snap CoordinatorState, r *ConsistencyReport, issues []string) []string {
	if snap.FetchedHeight >= snap.SyncedCursor {
		delta := snap.FetchedHeight - snap.SyncedCursor
		if delta <= math.MaxInt64 {
			r.MemoryDBGap = int64(delta) // #nosec G115 - delta bounded by MaxInt64 check above
		}
	} else {
		delta := snap.SyncedCursor - snap.FetchedHeight
		if delta <= math.MaxInt64 {
			r.MemoryDBGap = -int64(delta) // #nosec G115 - delta bounded by MaxInt64 check above
		}
	}
	if r.MemoryDBGap > 5000 {
		r.MemoryDBGapWarn = true
		r.MemoryDBGapDesc = fmt.Sprintf("Memory(%d) is %d blocks ahead of DB(%d)",
			snap.FetchedHeight, r.MemoryDBGap, snap.SyncedCursor)
		issues = append(issues, "MEMORY_DB_GAP: "+r.MemoryDBGapDesc)
	}
	return issues
}

// checkWatermarkInversion 检查水位线倒挂（FetchedHeight < SyncedCursor）
func (d *DebugServer) checkWatermarkInversion(snap CoordinatorState, r *ConsistencyReport, issues []string) []string {
	if snap.FetchedHeight < snap.SyncedCursor {
		r.WatermarkInversion = true
		r.WatermarkInversionDesc = fmt.Sprintf("FetchedHeight(%d) < SyncedCursor(%d)",
			snap.FetchedHeight, snap.SyncedCursor)
		issues = append(issues, "WATERMARK_INVERSION: "+r.WatermarkInversionDesc)
	}
	return issues
}

// checkCmdChanPressure 检查命令通道积压
func (d *DebugServer) checkCmdChanPressure(r *ConsistencyReport, issues []string) []string {
	cmdLen := len(d.orchestrator.cmdChan)
	cmdCap := cap(d.orchestrator.cmdChan)
	if cmdCap > 0 && cmdLen*100/cmdCap > 80 {
		r.CmdChanPressure = true
		r.CmdChanPressureDesc = fmt.Sprintf("CmdChan %d%% full (%d/%d)", cmdLen*100/cmdCap, cmdLen, cmdCap)
		issues = append(issues, "CMD_CHAN_PRESSURE: "+r.CmdChanPressureDesc)
	}
	return issues
}

// checkOracleDivergence 检查 HeightOracle 与 Orchestrator 高度不一致
func (d *DebugServer) checkOracleDivergence(snap CoordinatorState, oracle HeightOracleProvider, r *ConsistencyReport, issues []string) []string {
	chainHeadVal := oracle.ChainHead()
	if chainHeadVal < 0 || snap.LatestHeight == 0 {
		return issues
	}
	oracleChain := uint64(chainHeadVal) // #nosec G115 - guarded by >= 0 check above
	var diff uint64
	if oracleChain >= snap.LatestHeight {
		diff = oracleChain - snap.LatestHeight
	} else {
		diff = snap.LatestHeight - oracleChain
	}
	if diff > 100 {
		r.OracleOrchDivergence = true
		r.OracleOrchDivergenceDesc = fmt.Sprintf("HeightOracle(%d) vs Orchestrator(%d) diff=%d",
			oracleChain, snap.LatestHeight, diff)
		issues = append(issues, "ORACLE_DIVERGENCE: "+r.OracleOrchDivergenceDesc)
	}
	return issues
}

// checkGlobalStateDivergence 检查 GlobalState 与 Orchestrator 高度不一致
func (d *DebugServer) checkGlobalStateDivergence(snap CoordinatorState, gs Snapshot, r *ConsistencyReport, issues []string) []string {
	if gs.OnChainHeight == 0 || snap.LatestHeight == 0 {
		return issues
	}
	var diff uint64
	if gs.OnChainHeight >= snap.LatestHeight {
		diff = gs.OnChainHeight - snap.LatestHeight
	} else {
		diff = snap.LatestHeight - gs.OnChainHeight
	}
	if diff > 100 {
		r.GlobalStateOrchDivergence = true
		r.GlobalStateOrchDivergenceDesc = fmt.Sprintf("GlobalState(%d) vs Orchestrator(%d) diff=%d",
			gs.OnChainHeight, snap.LatestHeight, diff)
		issues = append(issues, "GLOBAL_STATE_DIVERGENCE: "+r.GlobalStateOrchDivergenceDesc)
	}
	return issues
}

// BuildComponentHealth 构建组件健康报告
func (d *DebugServer) BuildComponentHealth() ComponentHealthReport {
	now := time.Now().Format(time.RFC3339)
	var components []ComponentHealth
	overall := "ok"

	orchSnap := d.orchestrator.GetSnapshot()
	orchStatus := healthStatusOK
	orchMsg := fmt.Sprintf("state=%s latest=%d synced=%d", orchSnap.SystemState.String(), orchSnap.LatestHeight, orchSnap.SyncedCursor)
	if orchSnap.SystemState == SystemStateStalled || orchSnap.SystemState == SystemStateDegraded {
		orchStatus = healthStatusError
		overall = healthStatusCritical
	}
	components = append(components, ComponentHealth{Name: "orchestrator", Status: orchStatus, Message: orchMsg, CheckedAt: now})

	oracle := d.heightOracle()
	oracleStatus := healthStatusOK
	oracleMsg := fmt.Sprintf("chain=%d indexed=%d lag=%d stale=%dms", oracle.ChainHead(), oracle.IndexedHead(), oracle.SyncLag(), time.Since(oracle.UpdatedAt()).Milliseconds())
	if time.Since(oracle.UpdatedAt()) > 60*time.Second {
		oracleStatus = healthStatusWarn
		oracleMsg += " [STALE]"
		if overall == healthStatusOK {
			overall = healthStatusDegraded
		}
	}
	components = append(components, ComponentHealth{Name: "height_oracle", Status: oracleStatus, Message: oracleMsg, CheckedAt: now})

	gsSnap := d.globalState().Snapshot()
	gsMsg := fmt.Sprintf("onchain=%d db=%d state=%s", gsSnap.OnChainHeight, gsSnap.DBCursor, gsSnap.SystemState.String())
	components = append(components, ComponentHealth{Name: "global_state", Status: healthStatusOK, Message: gsMsg, CheckedAt: now})

	cmdLen := len(d.orchestrator.cmdChan)
	cmdCap := cap(d.orchestrator.cmdChan)
	chanStatus := healthStatusOK
	usagePct := 0
	if cmdCap > 0 {
		usagePct = cmdLen * 100 / cmdCap
	}
	chanMsg := fmt.Sprintf("len=%d cap=%d usage=%d%%", cmdLen, cmdCap, usagePct)
	if usagePct > 80 {
		chanStatus = healthStatusWarn
		if overall == healthStatusOK {
			overall = healthStatusDegraded
		}
	}
	components = append(components, ComponentHealth{Name: "cmd_channel", Status: chanStatus, Message: chanMsg, CheckedAt: now})

	fetcherStatus := healthStatusOK
	fetcherMsg := "wired"
	if d.orchestrator.fetcher == nil {
		fetcherStatus = healthStatusWarn
		fetcherMsg = "not wired (nil)"
		if overall == healthStatusOK {
			overall = healthStatusDegraded
		}
	}
	components = append(components, ComponentHealth{Name: "fetcher", Status: fetcherStatus, Message: fetcherMsg, CheckedAt: now})

	return ComponentHealthReport{Overall: overall, Components: components, CheckedAt: now}
}

// BuildPipelineTrace 构建数据流追踪
func (d *DebugServer) BuildPipelineTrace() map[string]interface{} {
	orchSnap := d.orchestrator.GetSnapshot()
	oracle := d.heightOracle()
	chainHead := oracle.ChainHead()
	latest := clampToInt64(orchSnap.LatestHeight)
	fetched := clampToInt64(orchSnap.FetchedHeight)
	synced := clampToInt64(orchSnap.SyncedCursor)

	lagChainToLatest := int64(0)
	if chainHead > latest {
		lagChainToLatest = chainHead - latest
	}
	lagLatestToFetched := int64(0)
	if latest > fetched {
		lagLatestToFetched = latest - fetched
	}
	lagFetchedToSynced := int64(0)
	if fetched > synced {
		lagFetchedToSynced = fetched - synced
	}

	return map[string]interface{}{
		"timestamp_ms": time.Now().UnixMilli(),
		"flow": []map[string]interface{}{
			{"stage": "1_rpc_chain_head", "value": chainHead, "source": "HeightOracle"},
			{"stage": "2_orchestrator_latest", "value": latest, "lag_from_prev": lagChainToLatest},
			{"stage": "3_fetcher_memory", "value": fetched, "lag_from_prev": lagLatestToFetched},
			{"stage": "4_db_synced", "value": synced, "lag_from_prev": lagFetchedToSynced},
		},
		"total_lag_blocks": SafeInt64Diff(orchSnap.LatestHeight, orchSnap.SyncedCursor),
	}
}

// BuildConfigAudit 构建配置审计
func (d *DebugServer) BuildConfigAudit() ConfigAuditResult {
	orchSnap := d.orchestrator.GetSnapshot()
	rp := d.orchestrator.GetRuntimeParams()

	result := ConfigAuditResult{
		RuntimeParams: rp,
		IsConsistent:  true,
	}

	if d.configManager != nil {
		cfg := d.configManager.Get()
		result.StaticConfig = cfg
		result.Warnings = auditConfigConsistency(cfg, orchSnap)
	}

	result.WarningCount = len(result.Warnings)
	result.IsConsistent = result.WarningCount == 0
	return result
}

// BuildRaceCheck 构建竞争状态检测
func (d *DebugServer) BuildRaceCheck() map[string]interface{} {
	snapshots := make([]CoordinatorState, 5)
	for i := range snapshots {
		snapshots[i] = d.orchestrator.GetSnapshot()
		time.Sleep(1 * time.Millisecond)
	}

	monotonicOk := true
	for i := 1; i < len(snapshots); i++ {
		if snapshots[i].SyncedCursor < snapshots[i-1].SyncedCursor {
			monotonicOk = false
			break
		}
	}

	last := snapshots[len(snapshots)-1]
	return map[string]interface{}{
		"timestamp_ms":            time.Now().UnixMilli(),
		"samples_taken":           len(snapshots),
		"synced_cursor_monotonic": monotonicOk,
		"race_detected":           !monotonicOk,
		"final_state": map[string]interface{}{
			"latest_height":  last.LatestHeight,
			"fetched_height": last.FetchedHeight,
			"synced_cursor":  last.SyncedCursor,
			"system_state":   last.SystemState.String(),
		},
	}
}

func (d *DebugServer) handleGoroutineDump(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(ExportFullGoroutineDump()))
}

func (d *DebugServer) handleGoroutineSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, GetLatestSnapshot())
}

// writeJSON 统一 JSON 响应写入
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		Logger.Error("debug_api_encode_failed", "err", err)
	}
}
