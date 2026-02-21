# 策略选择诊断与修复总结

## 问题描述

Web3 Indexer 在 Docker 环境（`make a2`）中错误地选择了 `PERSISTENT_TESTNET` 策略，而非配置文件中指定的 `EPHEMERAL_ANVIL`。

**预期行为**：
- `make test-a2`（本地 Go run，端口 8092）→ `EPHEMERAL_ANVIL` 策略
- `make a2`（Docker 容器，端口 8082）→ `EPHEMERAL_ANVIL` 策略

**实际行为**：
- `make test-a2` → ✅ `EPHEMERAL_ANVIL`（正确）
- `make a2` → ❌ `PERSISTENT_TESTNET`（错误）

---

## 根因分析

### 问题 1：Docker Compose 配置不完整

**文件**：`configs/docker/docker-compose.yml`

```yaml
# ❌ 修复前（缺少关键配置）
environment:
  - APP_TITLE=${APP_TITLE:-🚀 Web3 Indexer (Demo)}
  - PORT=${PORT:-8082}
  - FORCE_BEAST_ALIGNMENT=${FORCE_BEAST_ALIGNMENT:-true}
  - CLEAR_CHECKPOINTS_ON_START=${CLEAR_CHECKPOINTS_ON_START:-true}
  # ❌ 缺少 APP_MODE 和 CHAIN_ID 的显式设置！
```

**问题**：
- Docker 容器只通过 `env_file` 加载 `.env.demo2`
- 如果该文件中 `CHAIN_ID` 或 `APP_MODE` 配置不正确，就会导致策略选择错误
- 与 `test-a2` 命令行（显式覆盖 `APP_MODE` 和 `CHAIN_ID`）行为不一致

### 问题 2：策略工厂优先级不合理

**文件**：`internal/engine/factory.go`

```go
// ❌ 修复前（完全忽略 APP_MODE）
func NewStrategyFactoryFromChainID(chainID int64) *StrategyFactory {
    var mode string
    switch chainID {
    case 31337:
        mode = "EPHEMERAL_ANVIL"
    default:
        mode = "PERSISTENT_TESTNET"  // ← 走了这个分支
    }
    return &StrategyFactory{mode: mode}
}
```

**问题**：
- 即使 `.env.demo2` 设置了 `APP_MODE=EPHEMERAL_ANVIL`，如果 `cfg.ChainID` 不是 31337，就会走 default 分支
- 违反了"显式配置优先于隐式推断"的原则

---

## 修复方案

### Phase 1：Docker Compose 配置修复（必须）

**文件**：`configs/docker/docker-compose.yml`

```yaml
# ✅ 修复后（添加显式配置）
environment:
  - APP_TITLE=${APP_TITLE:-🚀 Web3 Indexer (Demo)}
  - PORT=${PORT:-8082}
  - APP_MODE=${APP_MODE:-EPHEMERAL_ANVIL}      # 🔥 新增
  - CHAIN_ID=${CHAIN_ID:-31337}                # 🔥 新增
  - FORCE_BEAST_ALIGNMENT=${FORCE_BEAST_ALIGNMENT:-true}
  - CLEAR_CHECKPOINTS_ON_START=${CLEAR_CHECKPOINTS_ON_START:-true}
```

**优点**：
- ✅ 与 `test-a2` 命令行行为一致
- ✅ 即使 `.env.demo2` 配置不完整也能正常工作
- ✅ 提供合理的默认值

### Phase 2：Go 代码防御性增强（推荐）

**文件**：`internal/engine/factory.go`

```go
// ✅ 修复后（APP_MODE 优先）
func NewStrategyFactoryFromChainID(chainID int64) *StrategyFactory {
    // 🔥 优先检查 APP_MODE 环境变量（用户显式意图）
    if appMode := os.Getenv("APP_MODE"); appMode != "" {
        slog.Info("🔍 StrategyFactory: APP_MODE override",
            "app_mode", appMode,
            "chain_id", chainID)
        return &StrategyFactory{mode: appMode}
    }

    // 否则使用 ChainID 自动判断
    var mode string
    switch chainID {
    case 31337:
        mode = "EPHEMERAL_ANVIL"
    // ...
    }
    return &StrategyFactory{mode: mode}
}
```

**优点**：
- ✅ 双重保险：Docker 配置 + Go 代码逻辑
- ✅ 符合"显式配置优先于隐式推断"原则
- ✅ 保持向后兼容（没有 APP_MODE 时仍用 ChainID）

### Phase 3：调试日志增强（可选）

**文件**：`cmd/indexer/main.go`

```go
// ✅ 新增诊断日志
slog.Info("🔍 [STRATEGY] Factory Initialization",
    "cfg_chain_id", cfg.ChainID,
    "env_chain_id", os.Getenv("CHAIN_ID"),
    "env_app_mode", os.Getenv("APP_MODE"),
    "env_rpc_urls", os.Getenv("RPC_URLS"))

factory := engine.NewStrategyFactoryFromChainID(cfg.ChainID)
strategy := factory.CreateStrategy()

slog.Info("🔍 [STRATEGY] Selected Strategy",
    "strategy_name", strategy.Name(),
    "factory_mode", factory.GetMode())
```

**优点**：
- ✅ 快速定位配置加载问题
- ✅ 可以后续保留用于生产调试
- ✅ 不影响业务逻辑

---

## 验证结果

### 编译检查
```bash
✅ go build ./cmd/indexer
# 无错误
```

### 配置检查
```bash
✅ CHAIN_ID=31337 (Anvil)
✅ APP_MODE=EPHEMERAL_ANVIL
```

### 代码检查
```bash
✅ docker-compose.yml 包含 APP_MODE 默认值
✅ docker-compose.yml 包含 CHAIN_ID 默认值
✅ factory.go 包含 APP_MODE 优先级检查
✅ main.go 包含策略工厂诊断日志
```

---

## 修改文件清单

### 必须修改
- ✅ **`configs/docker/docker-compose.yml:13-18`** — 添加 `APP_MODE` 和 `CHAIN_ID` 显式设置

### 推荐修改
- ✅ **`internal/engine/factory.go:68-82`** — 添加 `APP_MODE` 优先级检查

### 可选修改
- ✅ **`cmd/indexer/main.go:406-419`** — 添加调试日志

### 新增文件
- ✅ **`scripts/verify-strategy-fix.sh`** — 验证脚本

---

## 预期日志输出

### 修复前（错误）
```
🔍 StrategyFactory: ChainID detected as testnet chain_id=31337
🏭 StrategyFactory: Manufacturing [Sepolia-Eco] strategy mode=PERSISTENT_TESTNET
```

### 修复后（正确）
```
🔍 [STRATEGY] Factory Initialization cfg_chain_id=31337 env_chain_id=31337 env_app_mode=EPHEMERAL_ANVIL env_rpc_urls=http://127.0.0.1:8545
🔍 StrategyFactory: APP_MODE override app_mode=EPHEMERAL_ANVIL chain_id=31337
🏭 StrategyFactory: Manufacturing [Anvil-Speed] strategy mode=EPHEMERAL_ANVIL qps=1000 backpressure=5000
🔍 [STRATEGY] Selected Strategy strategy_name=EPHEMERAL_ANVIL factory_mode=EPHEMERAL_ANVIL
```

---

## 下一步验证步骤

### 1. 本地测试（test-a2）
```bash
make test-a2
curl -s http://localhost:8092/api/status | jq '.strategy'
# 预期: "EPHEMERAL_ANVIL"
```

### 2. Docker 测试（a2）
```bash
make a2
docker logs web3-demo2-app | grep 'StrategyFactory'
curl -s http://localhost:8082/api/status | jq '.strategy'
# 预期: "EPHEMERAL_ANVIL"
```

### 3. Goroutine 诊断
```bash
curl http://localhost:8082/debug/goroutines/dump
curl http://localhost:8082/debug/goroutines/snapshot
# 预期: 策略显示为 "EPHEMERAL_ANVIL"
```

---

## 风险评估

- **低风险**：
  - ✅ Docker 配置添加默认值（`${VAR:-default}`），不覆盖现有配置
  - ✅ Go 代码添加 `APP_MODE` 检查，向后兼容（没有时仍用 ChainID）

- **中风险**：
  - ⚠️ 修改策略工厂优先级，需要测试所有环境（Anvil、Sepolia、Mainnet）

- **缓解措施**：
  - ✅ 使用 `${VAR:-default}` 语法提供默认值
  - ✅ 保留 ChainID 作为 fallback
  - ✅ 添加详细的调试日志，便于排查问题

---

## 技术亮点

### 1. 双重保险设计
- Docker 配置（第一道防线）+ Go 代码逻辑（第二道防线）
- 即使一道防线失效，另一道仍能保证正确性

### 2. 显式配置优先
- `APP_MODE` 环境变量优先级高于 ChainID 自动推断
- 符合"用户显式意图 > 系统隐式推断"的设计原则

### 3. 防御性编程
- 所有默认值使用 `${VAR:-default}` 语法
- 保持向后兼容性（没有 APP_MODE 时仍用 ChainID）

### 4. 可观测性增强
- 详细的诊断日志，便于快速定位问题
- 验证脚本自动化检查配置正确性

---

## 总结

本次修复通过**三个层面的改进**解决了策略选择错误的问题：

1. **Docker 层**：添加显式环境变量配置，与本地环境保持一致
2. **Go 代码层**：优化策略工厂优先级，显式配置优先于隐式推断
3. **可观测性层**：添加诊断日志，便于快速定位问题

**核心原则**：
- ✅ 一致性：`test-a2` 和 `a2` 行为完全一致
- ✅ 健壮性：双重保险，防御性编程
- ✅ 可维护性：配置集中化，易于理解和修改

---

**修复日期**：2026-02-20
**修复状态**：✅ 已完成，等待运行时验证
**验证脚本**：`scripts/verify-strategy-fix.sh`
