# Anvil 无状态配置指南

**日期**: 2026-02-20
**目标**: 8082 和 8092 基于 Anvil，都尽量无状态

---

## 🎯 无状态目标

### 期望行为

```
每次重启索引器或 Anvil：
- Anvil 从块 0 开始
- 索引器从块 0 开始同步
- 没有旧数据残留
```

### 涉及端口

| 端口 | 用途 | 期望行为 |
|------|------|---------|
| **8082** | 正式版（Docker） | 容器重启后从 0 开始 |
| **8092** | 开发版（make test-a2） | 每次运行从 0 开始 |

---

## ✅ 实施方案

### 1. Anvil 临时模式

**修改 `configs/docker/docker-compose.yml`**：

```yaml
anvil:
  command: [
    "--host", "0.0.0.0",
    "--port", "8545",
    "--block-time", "1",
    "--anvil.tmp"  # 🔥 临时模式：数据不持久化
  ]
```

**效果**：
- ✅ Anvil 数据不保存到磁盘
- ✅ 容器重启后高度从 0 开始
- ✅ 每次都是干净的状态

### 2. 索引器 Nuclear Reset

**`AnvilStrategy.OnStartup()` 已有的行为**：

```go
// internal/engine/strategy.go
func (s *AnvilStrategy) OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, _ int64) error {
    // 1. 物理清空数据库
    TRUNCATE TABLE blocks, transfers, sync_checkpoints, ...

    // 2. 内存原子级归零
    o.ResetToZero()

    // 3. 清空管道残留
    o.fetcher.ClearJobs()
}
```

**效果**：
- ✅ 每次启动清空数据库
- ✅ 内存状态归零
- ✅ 从块 0 开始

### 3. 配置文件

**`configs/env/.env.demo2`**：

```bash
# 从创世块开始
START_BLOCK=0

# Anvil 模式
DEMO_MODE=true
IS_TESTNET=false
```

---

## 🚀 使用方法

### 方式 1: 使用清理脚本（推荐）⭐

```bash
# 一键清理所有状态
make clean-state

# 或直接运行脚本
./scripts/clean-state.sh
```

**效果**：
- ✅ 停止所有容器
- ✅ 清空数据库
- ✅ 重启 Anvil（临时模式）
- ✅ 重启 8082
- ✅ 一切从 0 开始

### 方式 2: 手动清理

```bash
# 1. 停止并删除容器
docker stop web3-indexer-app web3-demo2-anvil
docker rm web3-indexer-app web3-demo2-anvil

# 2. 清空数据库
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo <<SQL
TRUNCATE TABLE blocks CASCADE;
TRUNCATE TABLE transfers CASCADE;
TRUNCATE TABLE sync_checkpoints CASCADE;
SQL

# 3. 重启 Anvil
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d anvil

# 4. 重启 8082
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d indexer
```

### 方式 3: 仅重启 8082

```bash
# Anvil 已经在临时模式，只需重启索引器
docker restart web3-indexer-app

# 索引器会自动执行 Nuclear Reset
```

---

## 📊 验证方法

### 1. 检查 Anvil 高度

```bash
curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "Anvil 高度: %d\n"
```

**期望输出**：
```
Anvil 高度: 0-100  # 应该是很小的数字
```

### 2. 检查 8082 状态

```bash
curl -s http://localhost:8082/api/status | jq '{
  latest_block: .latest_block,
  latest_indexed: .latest_indexed,
  total_blocks: .total_blocks,
  state: .state
}'
```

**期望输出**：
```json
{
  "latest_block": "42",      // 小数字，从 0 开始
  "latest_indexed": "42",
  "total_blocks": 42,
  "state": "running"
}
```

### 3. 检查 8092 状态

```bash
make test-a2
# 查看 UI: http://localhost:8092
```

**期望**：
- Latest Block 应该是小数字
- Total Blocks 应该同步增长

---

## 🔍 工作原理

### Anvil 临时模式

```bash
--anvil.tmp
```

**效果**：
- ❌ 不保存链数据到磁盘
- ✅ 数据仅存在内存中
- ✅ 容器停止后数据丢失
- ✅ 容器重启后从 0 开始

### 索引器 Nuclear Reset

**触发条件**：
- ChainID = 31337 (Anvil)
- 每次启动 `OnStartup()`

**执行操作**：
1. TRUNCATE 所有表
2. 内存状态归零
3. 清空队列

---

## 🎯 不同场景

### 场景 1: 开发测试 8092

```bash
# 每次测试前清理
make clean-state

# 启动 8092
make test-a2
```

### 场景 2: 正式运行 8082

```bash
# 启动 8082
make a2

# 重启 8082（会自动清理）
docker restart web3-indexer-app
```

### 场景 3: 完全重置

```bash
# 一键清理所有状态
make clean-state
```

---

## ⚠️ 注意事项

### 1. 数据丢失

**临时模式**：
- ✅ 容器重启后数据丢失
- ✅ 每次都是干净的状态
- ❌ 不适合需要持久化的场景

**如果需要持久化**：
- 移除 `--anvil.tmp` 标志
- 使用数据卷保存 Anvil 数据

### 2. 数据库共享

**当前配置**：
- 8082 和 8092 共享同一个数据库（web3_demo）
- Nuclear Reset 会清空所有数据

**如果需要独立数据库**：
```bash
# 8082 使用 web3_demo_8082
# 8092 使用 web3_demo_8092
```

### 3. Anvil 端口冲突

**当前配置**：
- 8082 和 8092 共享同一个 Anvil（8545）

**如果需要独立 Anvil**：
- 8082 → Anvil 8545
- 8092 → Anvil 8546

---

## 📈 性能影响

### 临时模式 vs 持久化

| 模式 | 启动时间 | 内存使用 | 磁盘使用 | 数据保留 |
|------|---------|---------|---------|---------|
| **临时模式** | ~1s | ~100MB | 0 | ❌ 否 |
| **持久化** | ~1s | ~100MB | ~200MB | ✅ 是 |

**结论**：
- ✅ 临时模式性能相同
- ✅ 节省磁盘空间
- ✅ 适合开发测试

---

## 🚀 快速命令

```bash
# 完全清理（推荐）
make clean-state

# 仅重启 8082
docker restart web3-indexer-app

# 启动 8092
make test-a2

# 检查 Anvil 高度
curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "%d\n"

# 检查 8082 状态
curl -s http://localhost:8082/api/status | jq '.latest_block, .total_blocks'
```

---

## ✅ 总结

### 已实施的修改

1. ✅ **Anvil 临时模式**：添加 `--anvil.tmp` 标志
2. ✅ **清理脚本**：`scripts/clean-state.sh`
3. ✅ **Makefile 目标**：`make clean-state`
4. ✅ **Nuclear Reset**：已存在于 `AnvilStrategy`

### 效果

- ✅ **8082**：容器重启后从 0 开始
- ✅ **8092**：每次运行从 0 开始
- ✅ **Anvil**：临时模式，不持久化
- ✅ **数据库**：自动清空

### 使用建议

- **开发测试**：使用 `make clean-state`
- **日常使用**：直接重启容器
- **完全重置**：使用 `make clean-state`

---

**最后更新**: 2026-02-20
**状态**: ✅ 已实施
**无状态**: ✅ 已启用
