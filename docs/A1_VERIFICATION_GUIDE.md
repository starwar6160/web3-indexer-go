# Web3 Indexer - 测试网迁移验证手册

> **设计理念**：小步快跑（Small Increments）、原子化验证、环境隔离
>
> **目标**：实现从本地 Anvil 到 Sepolia 测试网的平滑迁移，彻底告别"考古模式"

---

## 📋 目录

1. [5 步原子化验证流程](#5-步原子化验证流程)
2. [快速开始](#快速开始)
3. [验证清单](#验证清单)
4. [故障排查](#故障排查)
5. [面试话术参考](#面试话术参考)

---

## 5 步原子化验证流程

### 步骤 1️⃣：RPC 连通性与额度预检

**目的**：在启动任何容器之前，验证 Sepolia RPC 节点是否可用

**操作**：
```bash
# 单独运行预检
make a1-pre-flight
```

**预期结果**：
```
========================================
步骤 1: RPC 连通性与额度预检
========================================
[INFO] 测试 RPC URL: https://eth-sepolia.g.alchemy.com/v2/...
[✅] RPC 连接成功
[INFO] 当前链头高度: 10262450 (0x9c986c)
[✅] 区块高度验证通过（千万量级）
```

**故障排查**：
- ❌ **RPC 请求失败** → 检查 API Key 是否正确
- ❌ **区块高度过低** → 确认是否连接到 Sepolia（非 Mainnet）
- ❌ **网络超时** → 检查防火墙和代理设置

---

### 步骤 2️⃣：数据库物理隔离验证

**目的**：确保 `web3_sepolia` 数据库独立存在，避免与 `make demo` 混淆

**验证点**：
- Testnet DB 端口：`15433`（Demo 使用 `15432`）
- 数据库名称：`web3_sepolia`（Demo 使用 `web3_indexer`）
- Docker 项目名：`web3-testnet`（Demo 使用 `web3-demo`）

**操作**：
```bash
# 检查数据库列表
docker exec -it web3-indexer-sepolia-db psql -U postgres -l | grep web3

# 预期输出：
# web3_indexer  (Demo 环境)
# web3_sepolia  (Testnet 环境)
```

**清理旧数据**（如需要）：
```bash
# 方案 1：完全重置（推荐）
make reset-a1

# 方案 2：仅清空表
make reset-testnet-db
```

---

### 步骤 3️⃣：起始高度解析逻辑验证

**目的**：验证 Go 程序能正确解析 `START_BLOCK=latest`，避免从创世块开始

**验证配置**：
```bash
# 检查 .env.testnet
cat .env.testnet | grep START_BLOCK
# 预期输出: START_BLOCK=latest

# 验证代码逻辑
grep -n "StartBlockStr == \"latest\"" cmd/indexer/main.go
# 预期输出: 第 35 行: if cfg.StartBlockStr == "latest" {
```

**验证演示模式硬编码**：
```bash
# 检查最小起始块 10262444
grep -n "10262444" cmd/indexer/main.go
# 预期输出: 多处匹配（包括 cfg.StartBlock = 10262444）
```

**预期启动日志**：
```
🎬 DEMO_MODE_ENABLED settings=...
🚀 STARTING_FROM_LATEST latest_block=10262450 checkpoint_block=19 lag=10262431
✅ WSS listener connected to wss://...
```

---

### 步骤 4️⃣：单步限流抓取测试

**目的**：验证令牌桶限流器是否生效，防止被测试网 Provider 封禁

**当前配置**：
```bash
cat .env.testnet | grep -E "RPC_RATE_LIMIT|FETCH_CONCURRENCY|MAX_SYNC_BATCH"
```

| 参数 | 值 | 说明 |
|------|-----|------|
| `RPC_RATE_LIMIT` | 1 | 每秒 1 次请求 |
| `FETCH_CONCURRENCY` | 2 | 2 个并发 Worker |
| `MAX_SYNC_BATCH` | 5 | 批次大小 5 块 |

**验证方式**：
```bash
# 启动索引器
make a1

# 观察日志（另开终端）
docker logs -f web3-indexer-sepolia-app
```

**预期特征**：
- ✅ 区块处理日志应该"有节奏"（约 1 秒 1 个）
- ✅ 无 `429 Too Many Requests` 错误
- ✅ 日志中看到 `TokenBucket` 或 `Rate Limit` 相关信息

**如果出现 429 错误**：
```bash
# 降低限流参数
vim .env.testnet
# 修改: RPC_RATE_LIMIT=0.5  # 每秒 0.5 次（2 秒 1 次）
```

---

### 步骤 5️⃣：可观测性链路回归

**目的**：确认数据正确流向 Dashboard 和 Prometheus

**验证指标**：
```bash
# 1. 检查 /metrics 端点
curl http://localhost:8081/metrics | grep indexer_current_height

# 预期输出（示例）：
# indexer_current_height{chain="11155111"} 10262450
```

**检查 Dashboard**：
```bash
# 访问 Dashboard
open http://localhost:8081  # macOS
xdg-open http://localhost:8081  # Linux

# 预期效果：
# 1. Sync Lag 在 0 附近跳动
# 2. E2E Latency < 60 秒（而非 1.3 亿秒）
# 3. Latest Blocks 显示千万量级高度（1026xxxx）
```

**验证 REST API**：
```bash
# 检查索引器状态
curl http://localhost:8081/api/status | jq '.'

# 预期输出（示例）：
{
  "last_synced_block": "10262450",
  "chain_id": 11155111,
  "e2e_latency_ms": 15230,  # 约 15 秒
  "sync_lag": 0
}
```

---

## 🚀 快速开始

### 场景 1：首次启动（推荐）

```bash
# 1. 配置 API Key（可选，如果 .env.testnet.local 不存在）
cat > .env.testnet.local <<EOF
SEPOLIA_RPC_URLS=https://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY
EOF

# 2. 运行预检（自动验证 5 步）
make a1-pre-flight

# 3. 启动测试网索引器
make a1

# 4. 查看日志
docker logs -f web3-indexer-sepolia-app
```

### 场景 2：快速重启（跳过预检）

```bash
# 直接启动（预检已通过）
docker compose -f docker-compose.testnet.yml -p web3-testnet up -d
```

### 场景 3：完全重置

```bash
# 清理所有容器和数据
make reset-a1

# 重新启动
make a1
```

---

## ✅ 验证清单

### 启动前验证（Pre-flight）

- [ ] RPC API Key 已配置（`.env.testnet.local` 或环境变量）
- [ ] 运行 `make a1-pre-flight` 全部通过
- [ ] 数据库端口无冲突（`15433` 未被占用）
- [ ] `.env.testnet` 中 `START_BLOCK=latest`

### 启动后验证（Post-flight）

- [ ] 容器状态正常：`docker ps | grep web3-testnet`
- [ ] 日志无错误：`docker logs web3-indexer-sepolia-app | grep -i error`
- [ ] 起始块高度正确：日志显示 `1026xxxx` 而非 `#1`
- [ ] 限流生效：日志处理间隔约 1 秒
- [ ] Dashboard 可访问：`http://localhost:8081`
- [ ] Metrics 端点正常：`curl http://localhost:8081/metrics`

### 数据验证（Data Integrity）

- [ ] 同步延迟 < 60 秒
- [ ] E2E Latency 合理（< 1 分钟）
- [ ] 最新区块高度接近链头（`1026xxxx`）
- [ ] 数据库中 `blocks` 表有记录
- [ ] `sync_checkpoints` 表正确更新

---

## 🔧 故障排查

### 问题 1：预检失败 - RPC 连接

**症状**：
```
[❌] RPC 请求失败
响应内容: {"error":"Invalid API Key"}
```

**解决方案**：
```bash
# 检查 API Key
cat .env.testnet.local | grep SEPOLIA_RPC_URLS

# 手动测试 RPC
curl -X POST -H "Content-Type: application/json" \
--data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY

# 重新配置 API Key
vim .env.testnet.local
```

### 问题 2：数据库连接失败

**症状**：
```
[ERROR] db_fail err=connection refused
```

**解决方案**：
```bash
# 检查数据库容器状态
docker ps | grep sepolia-db

# 检查数据库日志
docker logs web3-indexer-sepolia-db

# 重启数据库
docker restart web3-indexer-sepolia-db
```

### 问题 3：起始块从 0 开始

**症状**：
```
[INFO] Starting from block: 0
```

**解决方案**：
```bash
# 检查配置
cat .env.testnet | grep START_BLOCK

# 确认是 latest 而非数字
grep "START_BLOCK=latest" .env.testnet

# 重置数据库检查点
make reset-a1
```

### 问题 4：429 Too Many Requests

**症状**：
```
[WARN] RPC rate limit exceeded: 429
```

**解决方案**：
```bash
# 降低限流参数
vim .env.testnet
# 修改为: RPC_RATE_LIMIT=0.5

# 重启索引器
docker restart web3-indexer-sepolia-app
```

### 问题 5：E2E Latency 爆表

**症状**：
```
"e2e_latency_ms": 130000000  # 约 1.3 亿秒
```

**原因**：从创世块同步，导致时间跨度 4 年

**解决方案**：
```bash
# 1. 确认 START_BLOCK=latest
cat .env.testnet | grep START_BLOCK

# 2. 重置数据库
make reset-a1

# 3. 重新启动
make a1
```

---

## 💼 面试话术参考

**问题**：你是如何实现从本地到测试网的平滑迁移的？

> "在优化区块链索引器时，我设计了一个 5 步原子化验证流程：
>
> **1. RPC 预检**：在启动容器前，先用 `curl` 验证 Sepolia 节点连通性，避免无效启动。
>
> **2. 数据库隔离**：使用 Docker Project Name 实现环境隔离（testnet 用 `15433`，demo 用 `15432`），确保数据不混淆。
>
> **3. 起始高度解析**：实现 `START_BLOCK=latest` 动态解析，配合硬编码最小起始块 `10262444`，彻底告别'考古模式'。
>
> **4. 限流验证**：配置 QPS=1 的保守限流，观察日志处理节奏，防止触发 RPC 频率限制。
>
> **5. 可观测性回归**：通过 `/metrics` 端点和 Dashboard 验证数据流向，确认 E2E Latency 从 1.3 亿秒降至 < 60 秒。
>
> 所有验证步骤通过后，一条 `make a1` 命令即可启动测试网索引器。这种 **'Small Increments'** 的策略确保了系统的稳定性。"

**问题**：如何处理环境配置管理？

> "我使用 **'One Makefile, Multi-Environments'** 模式：
>
> - **环境隔离**：通过 Docker Project Name (`-p`) 实现容器级隔离
> - **配置分离**：`.env.testnet` 专门用于测试网，与 `.env` 解耦
> - **预检自动化**：`a1-pre-flight` 脚本在启动前自动验证 5 个关键检查点
> - **原子化操作**：每个验证步骤独立可执行，便于快速定位问题
>
> 这种设计体现了对 **'环境一致性'** 的极致追求，避免了配置漂移和环境污染。"

---

## 📚 附录

### A. 环境变量对照表

| 变量 | Demo | Testnet |
|------|------|---------|
| DB Name | `web3_indexer` | `web3_sepolia` |
| DB Port | `15432` | `15433` |
| API Port | `8080` | `8081` |
| Chain ID | `31337` (Anvil) | `11155111` (Sepolia) |
| START_BLOCK | `0` | `latest` |
| RPC_RATE_LIMIT | `200` | `1` |
| FETCH_CONCURRENCY | `10` | `2` |

### B. 常用命令速查

```bash
# 预检
make a1-pre-flight

# 启动
make a1

# 查看日志
make logs-testnet

# 重置
make reset-a1

# 检查状态
docker ps | grep web3-testnet

# 进入数据库
docker exec -it web3-indexer-sepolia-db psql -U postgres -d web3_sepolia

# 查看 Metrics
curl http://localhost:8081/metrics | grep indexer

# API 测试
curl http://localhost:8081/api/status | jq '.last_synced_block, .e2e_latency_ms'
```

---

**文档版本**：v1.0
**最后更新**：2026-02-15
**维护者**：追求 6 个 9 持久性的资深后端
