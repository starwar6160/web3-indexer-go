# 工业级监控与保护系统 - 实施计划

## 🎯 项目目标

为 Web3 Indexer 构建一套**生产级监控与保护系统**，确保在有限的商业节点配额下实现：
- 🛡️ **硬编码限流保护**（防止配置失误导致额度耗尽）
- 📊 **实时业务洞察**（代币转账量、额度使用率可视化）
- ⚠️ **智能额度预警**（80% 预警，90% 强限流）
- 🔄 **自动化部署**（一键同步 demo1/demo2 配置）

---

## 📋 实施阶段概览

| 阶段 | 任务 | 预计时间 | 风险等级 |
|------|------|----------|----------|
| **Phase 1** | 工业级限流保护（Go 代码） | 1 小时 | 低 |
| **Phase 2** | Prometheus 指标扩展（代币统计） | 1 小时 | 低 |
| **Phase 3** | 额度监控器实现（Go 代码） | 1.5 小时 | 中 |
| **Phase 4** | Grafana Dashboard 配置 | 1 小时 | 低 |
| **Phase 5** | Makefile 自动化部署 | 0.5 小时 | 低 |

**总预计时间**: ~5 小时

---

## Phase 1: 工业级限流保护（Go 代码）

### 目标
实现硬编码 RPS 上限保护，防止环境变量配置失误导致商业节点额度耗尽。

### 设计原则
1. **Fail-Safe 机制**: 默认采用最安全值（3 RPS）
2. **静默降级**: 检测到不安全配置时，强制降级而非崩溃
3. **审计日志**: 记录所有降级操作

### 实施步骤

#### Step 1.1: 创建限流器模块

**文件**: `internal/limiter/rate_limiter.go`（NEW）

```go
package limiter

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// 🛡️ 工业级硬编码保护
const (
	MaxSafetyRPS     = 3  // 绝对安全上限：每秒 3 次请求
	DefaultBurstSize = 1  // 允许 1 个并发突发
)

type RateLimiter struct {
	limiter *rate.Limiter
	maxRPS  int // 记录配置的 RPS（用于审计）
}

// NewRateLimiter 创建一个新的限流器
// 优先使用硬编码安全值，如果环境变量超过上限则强制降级
func NewRateLimiter(envRPS int) *RateLimiter {
	// 1. 默认采用硬编码的最安全值
	rps := MaxSafetyRPS

	// 2. 核心安全审计：如果外部传入的值超过了硬编码上限，强制降级
	if envRPS > 0 && envRPS <= MaxSafetyRPS {
		rps = envRPS
		slog.Info("✅ Rate limiter configured",
			"rps", rps,
			"mode", "safe")
	} else if envRPS > MaxSafetyRPS {
		slog.Warn("⚠️  Unsafe RPS config detected, forcing safe threshold",
			"requested_rps", envRPS,
			"forced_rps", MaxSafetyRPS,
			"reason", "commercial_quota_protection")
		rps = MaxSafetyRPS
	} else {
		slog.Info("✅ Rate limiter using default safe value",
			"rps", rps,
			"mode", "default")
	}

	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(rps), DefaultBurstSize),
		maxRPS:  rps,
	}
}

// Wait 阻塞直到获取令牌（或上下文取消）
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.limiter.Wait(ctx)
}

// MaxRPS 返回当前配置的最大 RPS（用于监控）
func (rl *RateLimiter) MaxRPS() int {
	return rl.maxRPS
}

// GetRPSEstimate 返回每秒实际消耗的 RPS 估算值
func (rl *RateLimiter) GetRPSEstimate() float64 {
	limit := float64(rl.limiter.Limit())
	burst := float64(rl.limiter.Burst())
	// 保守估算：限制值的 80% 作为实际 RPS
	return limit * 0.8
}
```

#### Step 1.2: 集成到 Fetcher

**文件**: `internal/engine/fetcher_core.go`（MODIFY）

**修改位置**: `NewFetcherWithLimiter` 函数（约第 70 行）

```go
// 在文件顶部添加导入
import "web3-indexer-go/internal/limiter"

// 修改 NewFetcherWithLimiter 函数
func NewFetcherWithLimiter(pool RPCClient, concurrency, rps, burst int) *Fetcher {
	// ✨ 使用工业级限流器（自动降级保护）
	rateLimiter := limiter.NewRateLimiter(rps)

	slog.Info("🛡️ Rate limiter initialized",
		"max_rps", rateLimiter.MaxRPS(),
		"concurrency", concurrency,
		"protection", "industrial_grade")

	f := &Fetcher{
		pool:        pool,
		concurrency: concurrency,
		jobs:        make(chan *big.Int, concurrency*2),
		Results:     make(chan BlockData, concurrency*2),
		limiter:     rateLimiter.limiter, // 使用内部限流器
		stopCh:      make(chan struct{}),
		paused:      false,
		metrics:     GetMetrics(),
	}
	f.pauseCond = sync.NewCond(&f.pauseMu)
	return f
}
```

### 验证清单

- [ ] 创建 `internal/limiter/rate_limiter.go`
- [ ] 修改 `internal/engine/fetcher_core.go`
- [ ] 测试：设置 `RPC_RATE_LIMIT=10`，查看日志是否降级到 3
- [ ] 测试：设置 `RPC_RATE_LIMIT=2`，查看日志是否正常使用 2
- [ ] 编译通过

---

## Phase 2: Prometheus 指标扩展（代币统计）

### 目标
扩展 Prometheus 指标，支持按代币类型统计转账量和次数。

### 实施步骤

#### Step 2.1: 扩展指标定义

**文件**: `internal/engine/metrics_core.go`（MODIFY）

**添加新指标**（约第 30 行后）：

```go
var (
	// ... 现有指标 ...

	// 📊 代币转账统计（按代币符号）
	TokenTransferVolume = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indexer_token_transfer_volume_total",
		Help: "Total volume of token transfers by token symbol (USDC, DAI, WETH, UNI)",
	}, []string{"symbol"})

	TokenTransferCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indexer_token_transfer_count_total",
		Help: "Total number of token transfers by token symbol",
	}, []string{"symbol"})
)

// RecordTokenTransfer 记录单笔代币转账
func (m *Metrics) RecordTokenTransfer(symbol string, amount *big.Int) {
	// 转换为浮点数（精度处理）
	amountFloat := float64(amount.Uint64()) / 1e18 // 假设 18 位小数

	TokenTransferVolume.WithLabelValues(symbol).Add(amountFloat)
	TokenTransferCount.WithLabelValues(symbol).Inc()

	// 同时记录到总转账量（保持向后兼容）
	m.TotalTransfers.Inc()
}
```

#### Step 2.2: 在 Processor 中调用

**文件**: `internal/engine/processor_block_part1.go`（MODIFY）

**修改位置**: 处理 Transfer Event 的逻辑（约第 100-150 行）

```go
// 在处理 Transfer Event 时添加
func (p *Processor) processTransferEvent(log types.Log, block *types.Block) error {
	// ... 现有代码 ...

	// 📊 记录代币转账统计
	tokenSymbol := getTokenSymbol(log.Address) // 需要实现这个函数
	p.metrics.RecordTokenTransfer(tokenSymbol, transferAmount)

	// ... 继续处理 ...
}

// getTokenSymbol 从代币地址映射到符号
func getTokenSymbol(tokenAddr common.Address) string {
	// Sepolia 热门代币地址映射
	tokenMap := map[string]string{
		"0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238": "USDC",
		"0xff34b3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357": "DAI",
		"0x7b79995e5f793A07Bc00c21412e50Ecae098E7f9": "WETH",
		"0xa3382DfFcA847B84592C05AB05937aE1A38623BC": "UNI",
	}

	hexAddr := tokenAddr.Hex()
	if symbol, ok := tokenMap[hexAddr]; ok {
		return symbol
	}
	return "Other" // 其他代币归类为 "Other"
}
```

### 验证清单

- [ ] 修改 `internal/engine/metrics_core.go`
- [ ] 修改 `internal/engine/processor_block_part1.go`
- [ ] 实现 `getTokenSymbol` 函数
- [ ] 访问 `http://localhost:8083/metrics`，查看新指标是否存在
- [ ] 等待 5 分钟，使用 `curl` 验证指标值在增长

---

## Phase 3: 额度监控器实现（Go 代码）

### 目标
实现智能额度预警系统，实时追踪 RPC 调用次数，在 80% 时预警，90% 时触发强限流。

### 实施步骤

#### Step 3.1: 创建额度监控器

**文件**: `internal/monitor/quota_monitor.go`（NEW）

```go
package monitor

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	MaxDailyQuota   = 300000 // 商业节点每日免费额度上限（CU）
	AlertThreshold  = 0.80   // 80% 预警阈值
	CriticalThreshold = 0.90 // 90% 临界阈值
)

type QuotaMonitor struct {
	dailyCalls  uint64      // 当天 RPC 调用次数
	resetTime   time.Time   // 下次重置时间（UTC 0 点）
	usageGauge  prometheus.Gauge
	statusGauge prometheus.Gauge
}

func NewQuotaMonitor() *QuotaMonitor {
	qm := &QuotaMonitor{
		usageGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "rpc_quota_usage_percent",
			Help: "Percentage of daily RPC quota used (0-100)",
		}),
		statusGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "rpc_quota_status",
			Help: "RPC quota status: 0=Safe, 1=Warning, 2=Critical",
		}),
	}
	qm.resetTime = qm.calculateNextReset()
	go qm.startResetTimer()
	return qm
}

// Inc 每次调用 RPC 前调用此方法
func (m *QuotaMonitor) Inc() {
	current := atomic.AddUint64(&m.dailyCalls, 1)
	usagePercent := float64(current) / float64(MaxDailyQuota)

	// 更新 Prometheus 指标
	m.usageGauge.Set(usagePercent * 100)

	// 更新状态指标
	status := 0.0 // Safe
	if usagePercent >= CriticalThreshold {
		status = 2.0 // Critical
	} else if usagePercent >= AlertThreshold {
		status = 1.0 // Warning
	}
	m.statusGauge.Set(status)

	// 阈值检查（每 100 次检查一次，避免日志刷屏）
	if current%100 == 0 {
		if usagePercent >= CriticalThreshold {
			slog.Error("🛑 CRITICAL: RPC quota nearly exhausted!",
				"usage_percent", usagePercent*100,
				"calls", current,
				"max_quota", MaxDailyQuota,
				"action", "consider_switching_to_idle_mode")
		} else if usagePercent >= AlertThreshold {
			slog.Warn("⚠️  QUOTA WARNING: RPC usage exceeds threshold",
				"usage_percent", usagePercent*100,
				"calls", current,
				"max_quota", MaxDailyQuota,
				"remaining", MaxDailyQuota-current)
		}
	}
}

// GetUsagePercent 返回当前使用率（0-100）
func (m *QuotaMonitor) GetUsagePercent() float64 {
	current := atomic.LoadUint64(&m.dailyCalls)
	return float64(current) / float64(MaxDailyQuota) * 100
}

// calculateNextReset 计算下一个 UTC 0 点
func (m *QuotaMonitor) calculateNextReset() time.Time {
	now := time.Now().UTC()
	nextReset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return nextReset
}

// startResetTimer 启动每日重置定时器
func (m *QuotaMonitor) startResetTimer() {
	for {
		now := time.Now().UTC()
		duration := m.resetTime.Sub(now)

		if duration > 0 {
			slog.Info("⏰ Quota monitor reset timer scheduled",
				"next_reset", m.resetTime.Format(time.RFC3339),
				"duration_hours", duration.Hours())
			time.Sleep(duration)
		}

		// 执行重置
		m.ResetDaily()
		m.resetTime = m.calculateNextReset()
	}
}

// ResetDaily 重置每日计数器（由定时任务调用）
func (m *QuotaMonitor) ResetDaily() {
	atomic.StoreUint64(&m.dailyCalls, 0)
	m.usageGauge.Set(0)
	m.statusGauge.Set(0)
	slog.Info("📅 Daily RPC quota counter reset",
		"time_utc", time.Now().UTC().Format(time.RFC3339))
}
```

#### Step 3.2: 集成到 RPC Client

**文件**: `internal/engine/rpc_pool_enhanced.go`（MODIFY）

**修改位置**: `Call` 方法中（约第 150 行）

```go
// 在文件顶部添加字段
type EnhancedRPCClientPool struct {
	// ... 现有字段 ...
	quotaMonitor *monitor.QuotaMonitor
}

// 修改构造函数
func NewEnhancedRPCClientPoolWithTimeout(urls []string, isTestnet bool, maxSyncBatch int, timeout time.Duration) (*EnhancedRPCClientPool, error) {
	// ... 现有代码 ...

	pool.quotaMonitor = monitor.NewQuotaMonitor()
	slog.Info("🛡️ Quota monitor initialized",
		"max_daily_quota", MaxDailyQuota,
		"alert_threshold", AlertThreshold*100,
		"critical_threshold", CriticalThreshold*100)

	return pool, nil
}

// 在每次 RPC 调用前调用
func (pool *EnhancedRPCClientPool) Call(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	// 📊 追踪额度使用
	pool.quotaMonitor.Inc()

	// ... 继续执行 RPC 调用 ...
}
```

### 验证清单

- [ ] 创建 `internal/monitor/quota_monitor.go`
- [ ] 修改 `internal/engine/rpc_pool_enhanced.go`
- [ ] 启动容器，查看日志是否有 "Quota monitor initialized"
- [ ] 等待 5 分钟，访问 `http://localhost:8083/metrics`，查看新指标
- [ ] 验证 `rpc_quota_usage_percent` 指标在增长

---

## Phase 4: Grafana Dashboard 配置

### 目标
创建两个新的 Grafana 面板：
1. **代币转账统计面板**（业务洞察）
2. **RPC 额度仪表盘**（资源监控）

### 实施步骤

#### Step 4.1: 代币转账统计面板

**文件**: `grafana/Token-Metrics-Dashboard.json`（NEW）

**包含的图表**：
1. **USDC 过去 1 小时流水**（Stat 面板）
   - PromQL: `sum(increase(indexer_token_transfer_volume_total{symbol="USDC"}[1h]))`
   - 单位: Currency (USD)

2. **四大热门代币转账次数**（Pie Chart）
   - PromQL: `sum by(symbol) (increase(indexer_token_transfer_count_total[24h]))`

3. **24 小时代币转账趋势**（Time Series）
   - PromQL: `sum by(symbol) (rate(indexer_token_transfer_count_total[5m]))`

#### Step 4.2: RPC 额度仪表盘

**配置参数**：
- **指标**: `rpc_quota_usage_percent`
- **面板标题**: `🛡️ RPC QUOTA GUARD (DAILY)`
- **单位**: Percent (0-100)
- **取值范围**: 0 / 100
- **颜色阈值**:
  - `0`: **Green** (安全)
  - `70`: **Yellow** (关注)
  - `90`: **Red** (临界)
- **展示风格**:
  - Show threshold markers: `On`
  - Text mode: `Value and name`

### 验证清单

- [ ] 创建 `grafana/Token-Metrics-Dashboard.json`
- [ ] 导入到 Grafana
- [ ] 验证 USDC 流水面板显示数据
- [ ] 验证额度仪表盘显示当前使用率
- [ ] 等待 10 分钟，确认数据在更新

---

## Phase 5: Makefile 自动化部署

### 目标
创建 Makefile 目标，一键同步 demo1 和 demo2 的面板配置。

### 实施步骤

#### Step 5.1: 扩展 Makefile

**文件**: `Makefile`（MODIFY）

**添加新目标**：

```makefile
# ============================================================================
# GRAFANA DASHBOARD MANAGEMENT
# ============================================================================

.PHONY: grafana-import-all
grafana-import-all: grafana-import-demo1 grafana-import-demo2
	@echo "✅ All Grafana dashboards imported successfully"

.PHONY: grafana-import-demo1
grafana-import-demo1:
	@echo "📊 Importing dashboards to demo1 (port 8081)..."
	@./scripts/import-grafana-dashboard.sh \
		--port=3001 \
		--dashboard=grafana/Web3-Indexer-Dashboard.json \
		--dashboard=grafana/Token-Analysis-Dashboard.json \
		--dashboard=grafana/Token-Metrics-Dashboard.json

.PHONY: grafana-import-demo2
grafana-import-demo2:
	@echo "📊 Importing dashboards to demo2 (port 8082)..."
	@./scripts/import-grafana-dashboard.sh \
		--port=3001 \
		--dashboard=grafana/Web3-Indexer-Dashboard.json

.PHONY: grafana-backup
grafana-backup:
	@echo "💾 Backing up all Grafana dashboards..."
	@mkdir -p backups/grafana
	@./scripts/export-grafana-dashboard.sh --output=backups/grafana

# ============================================================================
# QUOTA MANAGEMENT
# ============================================================================

.PHONY: quota-check
quota-check:
	@echo "🛡️ Checking RPC quota usage..."
	@curl -s http://localhost:8083/metrics | grep "rpc_quota_usage_percent"
	@curl -s http://localhost:8083/metrics | grep "rpc_quota_status"

.PHONY: quota-reset
quota-reset:
	@echo "🔄 Resetting daily quota counter (for testing only)..."
	@curl -X POST http://localhost:8083/api/admin/quota/reset
```

#### Step 5.2: 创建导入脚本

**文件**: `scripts/import-grafana-dashboard.sh`（NEW）

```bash
#!/bin/bash
# Grafana Dashboard 导入脚本

set -e

GRAFANA_HOST="localhost:3001"
GRAFANA_API_KEY="YOUR_API_KEY"  # 需要替换

while [[ $# -gt 0 ]]; do
	case $1 in
		--port)
			GRAFANA_HOST="localhost:$2"
			shift 2
			;;
		--dashboard)
			DASHBOARD="$2"
			echo "Importing $DASHBOARD..."
			curl -X POST "http://${GRAFANA_HOST}/api/dashboards/db" \
				-H "Content-Type: application/json" \
				-H "Authorization: Bearer ${GRAFANA_API_KEY}" \
				-d @"$DASHBOARD"
			shift 2
			;;
		*)
			echo "Unknown option: $1"
			exit 1
			;;
	esac
done

echo "✅ Dashboard import completed"
```

### 验证清单

- [ ] 修改 `Makefile`
- [ ] 创建 `scripts/import-grafana-dashboard.sh`
- [ ] 运行 `make grafana-import-demo1`
- [ ] 运行 `make quota-check`
- [ ] 验证两个环境的 Dashboard 一致

---

## 📁 最终文件清单

### 新增文件（7 个）

1. `internal/limiter/rate_limiter.go` - 工业级限流器
2. `internal/monitor/quota_monitor.go` - 额度监控器
3. `grafana/Token-Metrics-Dashboard.json` - 代币统计 Dashboard
4. `scripts/import-grafana-dashboard.sh` - Dashboard 导入脚本
5. `scripts/export-grafana-dashboard.sh` - Dashboard 导出脚本
6. `INDUSTRIAL_MONITORING_PLAN.md` - 本文档
7. `docs/04-Observability/quota-visualization.md` - 运维文档

### 修改文件（4 个）

1. `internal/engine/fetcher_core.go` - 集成限流器
2. `internal/engine/metrics_core.go` - 扩展指标定义
3. `internal/engine/processor_block_part1.go` - 记录代币转账
4. `internal/engine/rpc_pool_enhanced.go` - 集成额度监控
5. `Makefile` - 添加自动化目标

---

## 🎯 成功标准

### 功能验收
- ✅ RPS 硬编码上限保护（3 RPS）
- ✅ 代币转账指标可视化（USDC, DAI, WETH, UNI）
- ✅ RPC 额度实时监控（Gauge 面板）
- ✅ 80% 预警，90% 临界告警
- ✅ 每日自动重置（UTC 0 点）
- ✅ 一键同步 Dashboard 配置

### 性能验收
- ✅ 限流器延迟 < 1ms
- ✅ 额度监控开销 < 0.1% CPU
- ✅ Prometheus 指标查询 < 100ms
- ✅ Grafana 面板刷新 10 秒

### 运维验收
- ✅ 横滨时区适配（JST = UTC+9）
- ✅ 日志清晰记录所有降级操作
- ✅ Makefile 一键部署
- ✅ 两个环境（demo1/demo2）配置对齐

---

## 🚀 实施顺序建议

### Day 1: 核心保护（2 小时）
1. Phase 1: 工业级限流保护
2. Phase 2: Prometheus 指标扩展
3. 编译测试，验证基础功能

### Day 2: 监控完善（2 小时）
1. Phase 3: 额度监控器实现
2. Phase 4: Grafana Dashboard 配置
3. 导入 Dashboard，验证数据展示

### Day 3: 自动化（1 小时）
1. Phase 5: Makefile 自动化部署
2. 完整验证测试
3. 文档整理

---

## 📊 预期效果

### 演示效果（demo1）

**顶部**:
- System State: `● LIVE`
- Latest Blocks: 飞速跳动
- Real-time TPS: 7.75

**中部**:
- USDC 过去 1 小时流水: `$1,234.56`
- 四大热门代币占比: 饼图
- 24 小时转账趋势: 时间序列图

**底部**:
- 🛡️ RPC QUOTA GUARD: `15%` （绿色）
- Sync Lag: 136 块
- Enhanced RPC Pool: 2/2 healthy

### 技术价值

- ✅ **防御性编程**: 硬编码保护 + 静默降级
- ✅ **可观测性**: 实时业务指标 + 资源监控
- ✅ **自动化**: 一键部署 + 配置同步
- ✅ **工业级**: 6 个 9 持久性标准

---

**实施人员**: Claude Code
**项目状态**: ✅ 计划就绪
**最后更新**: 2026-02-16
