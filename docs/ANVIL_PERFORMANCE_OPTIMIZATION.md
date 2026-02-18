# Anvil 本地环境性能优化方案

## 📋 问题背景

在 Anvil 本地环境（ChainID=31337）下，Web3 Indexer 出现以下性能问题：

1. **Eco-Mode 误触发**: LazyManager 进入休眠，导致 `CRITICAL_STALL: Processor/MetadataEnricher blocked for 61s`
2. **数据库事务阻塞**: Processor 等待数据库连接或锁，导致 61 秒停滞
3. **数字倒挂**: UI 显示 `Total (Synced): 38828 > Latest (on Chain): 38823`，用户困惑

## 🎯 根本原因分析

| 问题 | 根本原因 | 证据位置 |
|------|----------|----------|
| Eco-Mode 误触发 | LazyManager 使用固定 5 分钟超时，无访客访问时自动休眠 | `internal/engine/lazy_manager.go:139` |
| 数据库阻塞 | 连接池配置为生产环境（25 连接），Anvil 高速写入导致竞争 | `cmd/indexer/main.go:211` |
| 数字倒挂 | HeightOracle.TailFollow 更新频率 500ms，Anvil 高速出块导致滞后 | `cmd/indexer/main.go:449` |

## ✅ 已有基础设施（可复用）

1. **环境检测**: `isLocalEnvironment()` 检测 localhost/127.0.0.1/anvil
2. **ChainID 识别**: `cfg.ChainID == 31337` 识别 Anvil
3. **LazyManager 接口**: `SetAlwaysActive(true)` 强制活跃
4. **LocalLabConfig**: 高性能配置模板（500 RPS, 16 并发）
5. **HeightOracle**: 单一真实源，避免竞态

## 🛡️ 三层防御体系

### 第一层：彻底禁用 Eco-Mode（Anvil 专属）

**目标**: 在 Anvil 环境下，强制 LazyManager 进入 "Lab Mode"，永不休眠。

**实现位置**: `cmd/indexer/main.go:277-283`

**当前代码**:
```go
// 🔥 Anvil 实验室环境：强制锁定为活跃状态，屏蔽休眠
// 优先级：ChainID 检测（自动）> FORCE_ALWAYS_ACTIVE（手动）
labModeEnabled := cfg.ChainID == 31337 || cfg.ForceAlwaysActive
if labModeEnabled {
    lazyManager.SetAlwaysActive(true)
    slog.Info("🔥 Lab Mode ACTIVATED: Eco-Mode disabled", "chain_id", cfg.ChainID, "force", cfg.ForceAlwaysActive)
}

// 🔥 更新 Prometheus 指标
engine.GetMetrics().SetLabMode(labModeEnabled)
```

**增强方案**:
- 新增环境变量 `FORCE_ALWAYS_ACTIVE` 支持手动覆盖
- 添加 Prometheus 指标 `indexer_lab_mode_enabled`
- 增强日志输出，明确标识 "Lab Mode ACTIVATED"

**预期效果**: Eco-Mode 误触发从频繁 → 0 次（100% 改善）

---

### 第二层：数据库连接池优化（环境感知）

**目标**: Anvil 环境使用激进连接池配置，消除事务阻塞。

**实现位置**: `cmd/indexer/main.go:324-350`

**当前代码**:
```go
func connectDB(ctx context.Context, isLocalAnvil bool) (*sqlx.DB, error) {
    dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
    defer cancel()
    db, err := sqlx.ConnectContext(dbCtx, "pgx", cfg.DatabaseURL)
    if err != nil {
        slog.Error("❌ Database connection failed", "err", err)
        return nil, err
    }

    if isLocalAnvil {
        // 🔥 Anvil 实验室配置：激进连接池（无限火力）
        db.SetMaxOpenConns(100)                 // 无限火力
        db.SetMaxIdleConns(20)                  // 保持热连接
        db.SetConnMaxLifetime(30 * time.Minute) // 更长生命周期
        db.SetConnMaxIdleTime(5 * time.Minute)
        slog.Info("🔥 Anvil database pool: 100 max connections (Lab Mode)")
    } else {
        // 🛡️ 生产环境：保守配置（安全第一）
        db.SetMaxOpenConns(25)
        db.SetMaxIdleConns(10)
        db.SetConnMaxLifetime(5 * time.Minute)
        db.SetConnMaxIdleTime(1 * time.Minute)
        slog.Info("🛡️ Production database pool: 25 connections, safety first")
    }

    return db, nil
}
```

**Prometheus 指标**:
```go
// internal/engine/metrics_core.go
var (
    dbPoolMaxConns = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "indexer_db_pool_max_connections",
        Help: "Maximum database connections configured",
    })
    dbPoolIdleConns = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "indexer_db_pool_idle_connections",
        Help: "Number of idle database connections",
    })
    dbPoolInUse = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "indexer_db_pool_in_use",
        Help: "Number of database connections currently in use",
    })
)
```

**预期效果**: 数据库事务阻塞从 61 秒 → < 1 秒（98% 改善）

---

### 第三层：实时高度更新（修复数字倒挂）

**目标**: 让 `Latest (on Chain)` 实时更新，消除"超前"困惑。

**问题**: 当前 TailFollow 更新频率 500ms（`cmd/indexer/main.go:469`），Anvil 高速出块导致滞后。

**实现方案**: 两步结合

#### 方案 1: 提高 TailFollow 频率
**位置**: `cmd/indexer/main.go:465-498`

```go
func continuousTailFollow(ctx context.Context, fetcher *engine.Fetcher, rpcPool engine.RPCClient, startBlock *big.Int) {
    slog.Info("🐕 [TailFollow] Starting continuous tail follow", "start_block", startBlock.String())
    lastScheduled := new(big.Int).Sub(startBlock, big.NewInt(1))

    // 🚀 工业级优化：本地 Anvil 实验室使用超高频轮询（100ms）
    tickerInterval := 500 * time.Millisecond
    if cfg.ChainID == 31337 {
        tickerInterval = 100 * time.Millisecond
        slog.Info("🔥 Anvil TailFollow: 100ms hyper-frequency update")
    }
    ticker := time.NewTicker(tickerInterval)

    // ... 其余代码不变
}
```

#### 方案 2: API 强制刷新（兜底保障）
**位置**: `cmd/indexer/api_handlers.go:121-136`

```go
func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool engine.RPCClient, lazyManager *engine.LazyManager, chainID int64, signer *engine.SignerMachine) {
    if lazyManager != nil {
        lazyManager.Trigger()
    }

    ctx := r.Context()

    // 🔥 Anvil 优化：每次 API 调用强制刷新高度，消除数字倒挂
    if chainID == 31337 {
        if tip, err := rpcPool.GetLatestBlockNumber(ctx); err == nil && tip != nil {
            engine.GetHeightOracle().SetChainHead(tip.Int64())
        }
    }

    snap := engine.GetHeightOracle().Snapshot()
    // ... 其余代码不变
}
```

**预期效果**: 数字倒挂现象从频繁 → 极少（80% 改善）

---

## 📦 实施步骤（原子提交策略）

### 提交 1: 环境感知数据库连接池优化

**文件**: `cmd/indexer/main.go`

**改动**:
1. 修改 `connectDB()` 函数签名，新增 `isLocalAnvil bool` 参数
2. 在函数内部实现环境感知的连接池配置
3. 修改调用点 `connectDB(ctx, cfg.ChainID == 31337)`
4. 同步修改回放模式调用点

**验证**:
```bash
# 检查日志
docker logs web3-indexer-app | grep "database pool"
# 预期: "🔥 Anvil database pool: 100 max connections (Lab Mode)"

# 检查连接池状态
curl http://localhost:8080/api/status | jq '.db_pool'
```

**回滚**: `git revert HEAD`

---

### 提交 2: 强制禁用 Eco-Mode（Anvil 专属）

**文件**: `internal/config/config.go`, `cmd/indexer/main.go`

**改动**:
1. `Config` 结构体新增 `ForceAlwaysActive bool` 字段
2. `Load()` 函数读取环境变量 `FORCE_ALWAYS_ACTIVE`
3. `main.go:277-283` 增强逻辑，支持 ChainID 或环境变量触发
4. 新增 Prometheus 指标 `indexer_lab_mode_enabled`

**验证**:
```bash
# 检查 LazyManager 状态
curl http://localhost:8080/api/status | jq '.lazy_indexer'
# 预期: {"mode": "active", "display": "🔥 Lab Mode: Engine Roaring"}

# 等待 5 分钟，验证不进入休眠
sleep 300
curl http://localhost:8080/api/status | jq '.lazy_indexer.mode'
# 预期: "active"（永不 "sleep"）
```

**回滚**: `git revert HEAD`

---

### 提交 3: 实时高度更新（修复数字倒挂）

**文件**: `cmd/indexer/api_handlers.go`, `cmd/indexer/main.go`

**改动**:
1. `api_handlers.go:121-136` 在 `handleGetStatus()` 中新增 Anvil 环境强制刷新逻辑
2. `main.go:465-498` 修改 TailFollow 频率，Anvil 使用 100ms

**验证**:
```bash
# 启动索引器，生成新区块
watch -n 1 'curl -s http://localhost:8080/api/status | jq "{latest: .latest_block, indexed: .latest_indexed}"'
# 预期: latest >= indexed（永不倒挂）

# 检查 TailFollow 频率
docker logs web3-indexer-app | grep "TailFollow"
# 预期: "🔥 Anvil TailFollow: 100ms hyper-frequency update"
```

**回滚**: `git revert HEAD`

---

### 提交 4: Prometheus 指标增强

**文件**: `internal/engine/metrics_core.go`, `internal/engine/metrics_methods.go`

**改动**:
1. 新增 Lab Mode 指标 `indexer_lab_mode_enabled`
2. 新增数据库连接池指标：
   - `indexer_db_pool_max_connections`
   - `indexer_db_pool_idle_connections`
   - `indexer_db_pool_in_use`
3. 在 `main.go` 调用点更新指标
4. 在 `service_manager.go` 中更新连接池状态

**验证**:
```bash
curl http://localhost:8080/metrics | grep indexer_lab_mode_enabled
# 预期: indexer_lab_mode_enabled 1

curl http://localhost:8080/metrics | grep indexer_db_pool_max_connections
# 预期: indexer_db_pool_max_connections 100
```

**回滚**: `git revert HEAD`

---

## 🧪 测试验证步骤

### Anvil 环境验证

```bash
# 1. 启动 Anvil 环境
make anvil-up

# 2. 启动索引器
docker-compose up -d web3-indexer-app

# 3. 检查日志
docker logs -f web3-indexer-app | grep -E "Lab Mode|database pool|TailFollow"

# 预期输出:
# 🔥 Anvil database pool: 100 max connections (Lab Mode)
# 🔥 Lab Mode ACTIVATED: Eco-Mode disabled
# 🔥 Anvil TailFollow: 100ms hyper-frequency update

# 4. 检查 LazyManager 状态
curl http://localhost:8080/api/status | jq '.lazy_indexer'

# 5. 检查数字是否倒挂
watch -n 1 'curl -s http://localhost:8080/api/status | jq "{latest: .latest_block, indexed: .latest_indexed}"'

# 6. 等待 5 分钟，验证不会进入休眠
sleep 300
curl http://localhost:8080/api/status | jq '.lazy_indexer.mode'

# 7. 压力测试（验证连接池）
for i in {1..1000}; do curl -s http://localhost:8080/api/blocks > /dev/null & done
wait
curl http://localhost:8080/metrics | grep indexer_db_pool
```

### Sepolia 测试网验证（环境隔离）

```bash
# 1. 启动 Sepolia 测试网
make a1

# 2. 检查日志（应使用生产配置）
docker logs -f web3-indexer-sepolia-app | grep -E "database pool|TailFollow"

# 预期输出:
# 🛡️ Production database pool: 25 connections, safety first
# (无 "Anvil TailFollow" 日志，使用默认 500ms)

# 3. 检查 LazyManager（应正常工作）
curl http://localhost:8081/api/status | jq '.lazy_indexer'

# 4. 等待 5 分钟，验证进入休眠
sleep 300
curl http://localhost:8081/api/status | jq '.lazy_indexer.mode'
# 预期: "sleep"（Eco-Mode 正常工作）
```

---

## 📊 预期效果

| 指标 | 优化前 | 优化后 | 改善 |
|------|--------|--------|------|
| **Eco-Mode 误触发** | 频繁（5 分钟） | 0 次（永不休眠） | 100% |
| **数据库事务阻塞** | 61 秒 | < 1 秒 | 98% |
| **数字倒挂现象** | 频繁（滞后 500ms） | 极少（100ms 刷新） | 80% |
| **连接池限制** | 25 连接（保守） | 100 连接（激进） | 300% |
| **人工干预** | 需要手动重启 | 0 次（自愈） | 100% |

---

## ⚠️ 风险评估和缓解措施

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|----------|
| **生产环境误用 Anvil 配置** | 连接池耗尽 | 低 | ChainID 严格检测 + 环境变量双重确认 |
| **高频 TailFollow 消耗 CPU** | 性能下降 | 中 | 仅 Anvil 环境 (100ms)，生产环境保持 500ms |
| **API 强制刷新增加 RPC 调用** | 触发限流 | 低 | 仅 Anvil 环境，生产环境不走此路径 |
| **连接池配置不当导致内存泄漏** | OOM | 极低 | 使用 `SetConnMaxLifetime` 自动回收 |

**回滚策略**:
- 每个提交独立可回滚：`git revert HEAD`
- 配置驱动：可通过环境变量立即禁用
- 环境隔离：ChainID 检测确保仅 Anvil 生效

---

## 🚀 环境变量配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CHAIN_ID` | 1 | 链 ID（31337 = Anvil） |
| `FORCE_ALWAYS_ACTIVE` | `false` | 强制禁用休眠（优先级低于 ChainID 检测） |

**使用示例**:
```bash
# Anvil 环境（自动检测）
CHAIN_ID=31337 make anvil-up

# 强制启用 Lab Mode（即使非 Anvil）
FORCE_ALWAYS_ACTIVE=true go run cmd/indexer/main.go
```

---

## 📚 相关文档

- **`NEVER_HIBERNATE_MODE.md`** - 永不休眠模式完整文档
- **`DEADLOCK_WATCHDOG_IMPLEMENTATION.md`** - 死锁看门狗实施报告
- **`ARCHITECTURE_ANALYSIS.md`** - 系统架构分析
- **`MEMORY.md`** - 项目记忆（第 200 行前）

---

## 📁 关键文件清单

### 核心改动文件

1. **`cmd/indexer/main.go`**
   - 第 324-350 行：`connectDB()` 修改函数签名，环境感知连接池
   - 第 277-283 行：LazyManager 集成逻辑增强 + Lab Mode 指标
   - 第 465-498 行：TailFollow 频率动态调整

2. **`cmd/indexer/api_handlers.go`**
   - 第 121-136 行：`handleGetStatus()` 强制刷新高度

3. **`internal/config/config.go`**
   - 新增 `ForceAlwaysActive` 字段
   - 第 110 行：读取环境变量

4. **`internal/engine/metrics_core.go`**
   - 新增 Lab Mode 和数据库连接池指标（第 51-58 行）

5. **`internal/engine/metrics_methods.go`**
   - 新增 `UpdateDBPoolStats()` 和 `SetLabMode()` 方法（第 98-115 行）

6. **`cmd/indexer/service_manager.go`**
   - 第 88-116 行：更新 `startMetricsReporter()` 方法

### 参考文件（无需修改）

- **`internal/engine/lazy_manager.go`** - 已有 `SetAlwaysActive()` 接口
- **`internal/engine/indexer_config.go`** - 已有 `LocalLabConfig()` 模板
- **`internal/engine/height_oracle.go`** - 已有 `SetChainHead()` 方法
- **`internal/limiter/rate_limiter.go`** - 已有 `isLocalEnvironment()` 检测

---

**最后更新**: 2026-02-19
**维护者**: 追求 6 个 9 持久性的资深后端工程师
