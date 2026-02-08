# 🧪 Anvil Testing Guide - Local Controlled Environment

> **SRE 最佳实践**: 在核心逻辑未完全验证前，隔离并控制外部不可靠的依赖。

## 为什么选择 Anvil？

| 环境 | Sepolia (QuickNode) | Anvil (本地容器) | 结论 |
| :--- | :--- | :--- | :--- |
| **可用性** | 外部网络延迟，限流风险高 | 100% 局域网内，无延迟，永不限流 | **✅ 高控** |
| **数据** | 真实，但写入速率不可控 | 模拟，但数据可以预制，可控 | **✅ 高控** |
| **调试** | 无法单步调试 RPC 响应 | 可以断点追踪 Go 进程与 Anvil 容器的交互 | **✅ 易于调试** |
| **成本** | 需要 API Key，有限流风险 | 完全免费，无限制 | **✅ 零成本** |

## 快速开始

### 方式 1: 使用 Makefile（推荐）

```bash
# 启动 Anvil 演示环境（包含合约部署和测试交易）
make demo

# 然后在另一个终端启动索引器
DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable \
RPC_URLS=http://localhost:8545 \
CHAIN_ID=31337 \
START_BLOCK=0 \
LOG_LEVEL=debug \
./bin/indexer

# 停止 Anvil
make anvil-down
```

### 方式 2: 使用脚本

```bash
# 一键运行完整测试流程
./scripts/anvil-test.sh
```

### 方式 3: 手动控制

```bash
# 启动 Anvil + PostgreSQL
make anvil-up

# 部署演示合约
make demo-deploy

# 编译索引器
make build

# 启动索引器
DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable \
RPC_URLS=http://localhost:8545 \
CHAIN_ID=31337 \
START_BLOCK=0 \
LOG_LEVEL=debug \
./bin/indexer

# 在另一个终端停止
make anvil-down
```

## 核心逻辑验证清单

### ✅ 第一步：RPC 连接验证
```bash
# 检查 Anvil 是否响应
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'

# 预期响应：
# {"jsonrpc":"2.0","result":"0x7a69","id":1}  (31337 in hex)
```

### ✅ 第二步：合约部署验证
```bash
# 部署演示合约并发送 10 笔交易
make demo-deploy

# 预期输出：
# ✅ Connected to Anvil (Chain ID: 31337)
# 📝 Deploying from: 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266
# 🚀 Deploying ERC20 contract...
# ✅ Contract deployed at: 0x...
# 📤 Sending test transactions...
# ✅ TX 1 sent: 0x...
# ... (10 transactions total)
```

### ✅ 第三步：Sequencer 初始化验证
启动索引器并观察日志：

```bash
./bin/indexer
```

**关键日志指标**：
```
✅ configuration_loaded - RPC URLs 和 Chain ID 正确加载
✅ rpc_pool_initialized - RPC 池健康节点数 > 0
✅ blocks_scheduled - Fetcher 成功调度区块范围
✅ sequencer_started - Sequencer 成功启动
✅ smart_sleep_system_enabled - 状态管理器启动
```

**问题诊断**：
- 如果看到 `sequencer not initialized` 错误 → Sequencer 初始化失败，检查日志中的 `sequencer_started` 是否出现
- 如果看到 `rpc_pool_init_failed` → RPC 连接失败，检查 Anvil 是否运行
- 如果看到 `database_connection_failed` → PostgreSQL 连接失败，检查数据库配置

### ✅ 第四步：数据处理验证
观察 Sequencer 处理区块的日志：

```
📦 Sequencer received block: 1
📦 Sequencer received block: 2
...
```

**期望行为**：
- 区块按顺序处理（1, 2, 3, ...）
- 没有乱序或重复
- Buffer 大小保持在合理范围（< 100）

### ✅ 第五步：健康检查验证
```bash
# 在另一个终端运行
curl http://localhost:8080/healthz | jq .

# 预期响应：
{
  "status": "healthy",
  "timestamp": "2024-02-08T21:30:00Z",
  "checks": {
    "database": {"status": "healthy", "latency": "5ms"},
    "rpc": {"status": "healthy", "message": "rpc_nodes: 1/1 healthy, latest_block: 10"},
    "sequencer": {"status": "healthy", "message": "expected_block: 11, buffer_size: 0"},
    "fetcher": {"status": "healthy", "message": "fetcher running"}
  }
}
```

## 环境变量配置

### Anvil 专用配置
```bash
# 必需
RPC_URLS=http://localhost:8545
CHAIN_ID=31337
START_BLOCK=0

# 可选但推荐
LOG_LEVEL=debug              # 调试模式，查看详细日志
DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable
RPC_TIMEOUT_SECONDS=10
```

### 与 Sepolia 的对比
```bash
# Sepolia 配置（生产环境）
RPC_URLS=https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY
CHAIN_ID=11155111
START_BLOCK=5000000

# Anvil 配置（本地测试）
RPC_URLS=http://localhost:8545
CHAIN_ID=31337
START_BLOCK=0
```

## 常见问题排查

### Q1: "sequencer not initialized" 错误
**原因**: Sequencer 未能正确初始化
**解决**:
1. 检查日志中是否有 `sequencer_started` 消息
2. 确认 RPC 连接正常：`make anvil-up` 后运行 `curl http://localhost:8545`
3. 检查 PostgreSQL 是否运行：`docker ps | grep postgres`

### Q2: "rpc_pool_init_failed" 错误
**原因**: RPC 连接失败
**解决**:
1. 确认 Anvil 运行中：`docker ps | grep anvil`
2. 测试连接：`curl -X POST http://localhost:8545 -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'`
3. 检查防火墙：`sudo ufw allow 8545`

### Q3: "database_connection_failed" 错误
**原因**: PostgreSQL 连接失败
**解决**:
1. 确认 PostgreSQL 运行中：`docker ps | grep postgres`
2. 检查连接字符串：`DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable`
3. 重启数据库：`make anvil-down && make anvil-up`

### Q4: Sequencer buffer 不断增长
**原因**: 前面的区块未被处理，导致后续区块堆积
**解决**:
1. 检查 Fetcher 是否正常运行：查看日志中的 `blocks_scheduled`
2. 检查 Processor 是否有错误：查看数据库写入是否成功
3. 增加日志级别：`LOG_LEVEL=debug`

## 集成测试

### 运行集成测试（使用 Anvil）
```bash
make test-anvil
```

这将：
1. 启动 Anvil + PostgreSQL
2. 运行所有标记为 `integration` 的测试
3. 自动清理环境

### 编写集成测试
```go
// internal/engine/sequencer_integration_test.go
// +build integration

package engine

import (
    "context"
    "testing"
)

func TestSequencer_WithAnvil(t *testing.T) {
    // 连接到 http://localhost:8545
    // 部署合约
    // 发送交易
    // 验证 Sequencer 处理
}
```

## 性能基准

在本地 Anvil 上的预期性能：

| 指标 | 值 |
| :--- | :--- |
| **RPC 延迟** | < 1ms |
| **Sequencer 处理速度** | > 1000 blocks/sec |
| **内存占用** | < 100MB |
| **CPU 使用** | < 5% |

## 演示策略

### 面试演示流程
```bash
# 1. 启动本地环境
make demo

# 2. 在另一个终端启动索引器
./bin/indexer

# 3. 打开浏览器
open http://localhost:8080

# 4. 观察实时数据处理
# - Dashboard 显示实时状态
# - 日志显示区块处理
# - 健康检查显示所有组件就绪
```

### 演讲稿
> "我使用 Anvil 本地模拟链来验证 Go 索引器的核心逻辑。这样做有三个优势：
> 
> 1. **完全控制**: 所有数据都是可预制的，没有外部依赖
> 2. **快速反馈**: RPC 延迟 < 1ms，可以快速迭代
> 3. **可重现**: 每次运行都是相同的环境，便于调试
> 
> 一旦核心逻辑在本地通过验证，切换到 Sepolia 只需修改一个环境变量。"

## 下一步：切换到 Sepolia

当本地 Anvil 测试通过后，切换到 Sepolia 非常简单：

```bash
# 只需修改环境变量
DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable \
RPC_URLS=https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY \
CHAIN_ID=11155111 \
START_BLOCK=5000000 \
LOG_LEVEL=info \
./bin/indexer
```

核心逻辑完全相同，只是数据源不同。

## 相关命令速查

```bash
# 启动/停止
make anvil-up          # 启动 Anvil + PostgreSQL
make anvil-down        # 停止 Anvil + PostgreSQL
make demo              # 完整演示（启动 + 部署 + 提示）
make verify            # 快速验证（启动 + 部署 + 运行 30 秒）

# 部署
make demo-deploy       # 部署合约和测试交易
make build             # 编译索引器

# 测试
make test-anvil        # 运行集成测试
go test -v ./...       # 运行所有单元测试

# 日志和状态
make logs              # 查看服务日志
make status            # 查看服务状态
curl http://localhost:8080/healthz | jq .  # 健康检查
```

---

**🎯 核心原则**: 先在本地 Anvil 上验证 Go 引擎的稳定性和原子性，然后告诉面试官系统已在受控环境下完全验证。
