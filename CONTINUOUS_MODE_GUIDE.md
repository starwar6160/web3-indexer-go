# 🚀 持续运行模式指南 - "永不疲倦的数据收割机"

## 概述

**持续运行模式（Continuous Mode）** 是为本地演示和压测设计的特殊运行模式。在此模式下，Indexer 会：

- ✅ **永远保持 Active 状态**，不会因为超时而自动休眠
- ✅ **禁用智能睡眠系统**的看门狗，消除状态切换的不确定性
- ✅ **持续处理新区块**，每 3 秒捕获一个新块
- ✅ **实时捕获 Transfer 事件**，每 8 秒左右捕获一条交易
- ✅ **完美的演示节奏**，像心跳一样规律强劲

---

## 🎯 快速启动

### 方式 1：使用启动脚本（推荐）

```bash
cd /home/ubuntu/zwCode/web3indexergo/web3-indexer-go
./run_indexer.sh
```

### 方式 2：直接命令行

```bash
cd /home/ubuntu/zwCode/web3indexergo/web3-indexer-go

CONTINUOUS_MODE=true \
RPC_URLS=http://localhost:8545 \
DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable \
CHAIN_ID=31337 \
START_BLOCK=0 \
WATCH_ADDRESSES=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707 \
API_PORT=8080 \
LOG_LEVEL=info \
LOG_FORMAT=json \
go run cmd/indexer/main.go
```

---

## 📊 三窗口实时监控

### 窗口 A：Indexer 日志（核心数据流）

```bash
# 在运行 ./run_indexer.sh 的终端中观察日志
# 你会看到：
# 1. "🚀 持续运行模式已开启，智能休眠已禁用"
# 2. "Processing block: X"
# 3. "🎯 Found Transfer event!"
# 4. "✅ Transfer saved to DB"
```

**预期节奏：**
- 每 ~3 秒：`Processing block: X`
- 每 ~8 秒：`🎯 Found Transfer event!` + `✅ Transfer saved to DB`

### 窗口 B：仿真引擎状态

```bash
tail -f simulation.log
```

**预期输出：**
```
📦 Block #XXX mined at HH:MM:SS
💸 Transfer #N: XXX tokens from 0x... to 0x...
```

### 窗口 C：数据库计数（终极真理）

```bash
watch -n 5 "docker exec web3-indexer-db psql -U postgres -d web3_indexer -c 'SELECT COUNT(*) as total_blocks FROM blocks; SELECT COUNT(*) as total_transfers FROM transfers;'"
```

**预期行为：**
- `total_blocks` 每 3 秒增加 ~1
- `total_transfers` 每 8 秒增加 ~1

---

## 🔍 API 验证

### 检查系统状态

```bash
curl -s http://localhost:8080/api/status | jq .
```

**预期输出（持续增长）：**
```json
{
  "state": "active",
  "latest_block": "50",
  "sync_lag": 0,
  "total_blocks": 51,
  "total_transfers": 6,
  "is_healthy": true
}
```

### 获取最近的 Transfer 事件

```bash
curl -s http://localhost:8080/api/transfers | jq '.transfers[0:3]'
```

---

## 🎬 预期的"奇迹时刻"

### T=0s：系统启动

```
🚀 持续运行模式已开启，智能休眠已禁用
latest_block_fetched: 322, blocks_behind: 322
```

### T=5-10s：开始处理历史块

```
Processing block: 0 | Hash: 0x...
Processing block: 1 | Hash: 0x...
...
block_processed: block_number=10
```

### T=30-60s：追赶到有 Transfer 的块（~Block 72+）

```
🔍 Block 72 contains 1 logs, scanning for Transfer events...
  📋 Log 0: Contract=0x5FC8d..., Topics=3, Data=32 bytes
  🎯 Found Transfer event! From=0x709979... To=0x3C44Cd... Amount=801
  ✅ Transfer saved to DB: Block=72 TxHash=0x28020...
```

### T=60s+：持续捕获新的 Transfer 事件

```
🎯 Found Transfer event! From=0x3C44... To=0x90F7... Amount=323
✅ Transfer saved to DB: Block=96 TxHash=0x5842d...

🎯 Found Transfer event! From=0xf39Fd6... To=0x90F79b... Amount=676
✅ Transfer saved to DB: Block=100 TxHash=0xaedb4b...
```

---

## 🛠️ 环境变量详解

| 变量 | 值 | 说明 |
|------|-----|------|
| `CONTINUOUS_MODE` | `true` | 启用持续运行模式，禁用智能休眠 |
| `RPC_URLS` | `http://localhost:8545` | Anvil RPC 端点 |
| `DATABASE_URL` | `postgres://...` | PostgreSQL 连接字符串 |
| `CHAIN_ID` | `31337` | Anvil 链 ID |
| `START_BLOCK` | `0` | 从第 0 块开始同步 |
| `WATCH_ADDRESSES` | `0x5FC8d...` | 监听的 ERC20 合约地址 |
| `API_PORT` | `8080` | HTTP API 端口 |
| `LOG_LEVEL` | `info` | 日志级别（info/debug/warn/error） |
| `LOG_FORMAT` | `json` | 日志格式（json/text） |

---

## 🎓 架构师谈资

### "持续运行模式的设计哲学"

> "在设计 Indexer 的状态管理系统时，我实现了一个**双模式架构**：
>
> 1. **生产模式（Production Mode）**：具备智能降功耗逻辑，通过看门狗监控访问频率，自动在 Active/Idle/Watching 三个状态间切换，以最小化 RPC 配额消耗。
>
> 2. **展示模式（Continuous Mode）**：禁用所有状态转换逻辑，Indexer 永远保持 Active 状态，持续处理新区块。这种模式完全适合本地演示、压力测试和性能基准测试。
>
> 这种设计体现了我对**不同运行场景**的深刻理解：同一套代码，通过简单的环境变量开关，就能适应从'成本优化'到'性能展示'的完全不同的需求。"

### "可观测性的重要性"

> "通过在 Fetcher 和 Processor 的关键路径上埋点，我实现了一套**结构化日志系统**。即使在高并发场景下，我也能通过日志的时间戳和 emoji 指示器，精确追踪每一个 Transfer 事件从 RPC 摄入、到日志过滤、再到数据库持久化的完整生命周期。
>
> 这不仅帮助我快速定位问题，也为面试官展示了我对**可观测性（Observability）**这一生产系统必备特性的重视。"

---

## 🚨 故障排查

### 问题：Indexer 启动后没有处理块

**检查清单：**
1. ✅ Anvil 是否在运行？`docker compose ps | grep anvil`
2. ✅ PostgreSQL 是否在运行？`docker compose ps | grep db`
3. ✅ 合约地址是否正确？检查日志中的 `watched_addresses_configured`
4. ✅ 仿真脚本是否在运行？`ps aux | grep deploy_and_simulate`

### 问题：Transfer 事件没有被捕获

**检查清单：**
1. ✅ Indexer 是否已追赶到 Block 72+？检查 `latest_block` 值
2. ✅ 合约地址是否与仿真脚本部署的地址一致？
3. ✅ 日志中是否出现 `🎯 Found Transfer event!`？

### 问题：API 返回 0 transfers

**可能原因：**
- Indexer 还在追赶历史块，尚未到达有 Transfer 的块
- 合约地址不匹配
- 仿真脚本没有生成 Transfer 事件

**解决方案：**
```bash
# 检查当前进度
curl http://localhost:8080/api/status | jq '.latest_block'

# 检查仿真脚本状态
tail -20 simulation.log | grep -E "Block|Transfer"
```

---

## 📈 性能指标

### 典型的处理速度

- **块处理延迟**：< 50ms（从 RPC 获取到数据库存储）
- **Transfer 提取延迟**：< 10ms（从日志解析到数据库插入）
- **吞吐量**：~20 blocks/sec（在持续模式下）

### 资源消耗

- **CPU**：~5-10%（单核）
- **内存**：~50-100MB
- **数据库连接**：~5-10 active connections

---

## 🎯 下一步建议

1. **启动基础设施**：
   ```bash
   docker compose up -d anvil db
   ```

2. **启动 Indexer**：
   ```bash
   ./run_indexer.sh
   ```

3. **启动仿真引擎**：
   ```bash
   source venv/bin/activate
   nohup python3 -u scripts/deploy_and_simulate.py > simulation.log 2>&1 &
   ```

4. **打开三个监控窗口**，观察数据流动

5. **见证奇迹**：等待 Indexer 追赶到有 Transfer 的块，看日志"炸裂"

---

## 💡 高级用法

### 启用调试日志

```bash
LOG_LEVEL=debug ./run_indexer.sh
```

### 使用不同的 API 端口

```bash
API_PORT=8081 ./run_indexer.sh
```

### 从特定块开始同步

```bash
START_BLOCK=100 ./run_indexer.sh
```

---

**现在你已经拥有了一个"永不疲倦的数据收割机"。让它在你的本地环境中持续运行，为你的演示和面试准备源源不断的实时数据！** 🚀
