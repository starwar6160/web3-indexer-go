# 8082 端口数据丢失问题 - 诊断与解决

**日期**: 2026-02-20
**问题**: 8082 端口（正式版 Docker）没有数据
**原因**: Anvil 重置导致的"时空悖论"

---

## 🚨 问题现象

### 用户报告
```
8082 端口：正式发布版本（Docker）
8092 端口：实时运行版本（make test-a2）

问题：8082 下面没有数据
```

### 实际状态
```json
{
  "latest_block": "2989",
  "latest_indexed": "0",
  "total_blocks": 0,
  "total_transfers": 0,
  "state": "stalled"
}
```

---

## 🔍 根本原因

### 时间线

```
1. 昨天（2月19日）
   - 8082 容器启动，Anvil 高度 ~35000
   - 索引器同步到 37307

2. 今天（2月20日 07:56）
   - 为了重置 8092，删除了 Anvil 容器
   - 8082 失去 Anvil 连接
   - 日志：TailFollow: Failed to get tip, connection refused

3. Anvil 重新启动
   - 从 0 开始，高度只有 2989
   - 8082 的 Sequencer 还在等 37307
   - 数据库被 Nuclear Reset 清空

4. 结果
   - 8082 stalled
   - 索引器在 37307，Anvil 在 2989
   - "时空悖论"
```

### 为什么数据库被清空？

**`AnvilStrategy.OnStartup()` 的 Nuclear Reset**：

```go
// internal/engine/strategy.go:25-50
func (s *AnvilStrategy) OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, _ int64) error {
    // 1. 物理清空数据库
    if db != nil {
        tables := []string{"blocks", "transfers", "sync_checkpoints", ...}
        for _, table := range tables {
            db.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
        }
    }

    // 2. 内存原子级归零
    o.ResetToZero()

    // 3. 清空管道残留
    o.fetcher.ClearJobs()
}
```

**问题**：
- ✅ Anvil 环境应该使用 Nuclear Reset
- ❌ 但我删除 Anvil 容器时，8082 还在运行
- ❌ 8082 重启后触发了 Nuclear Reset，清空了数据库

---

## ✅ 解决方案

### 方案 1: 重启 8082 容器（已执行）⭐

```bash
docker restart web3-indexer-app
```

**效果**：
- ✅ 重新从当前 Anvil 高度（2989）开始同步
- ✅ 数据库自动重建
- ✅ 状态恢复正常

**重启后状态**：
```json
{
  "latest_block": "3303",
  "latest_indexed": "3302",
  "total_blocks": 3302,
  "state": "running",
  "sync_lag": 1
}
```

### 方案 2: 避免影响 8082 的 Anvil 重置

**修改 `scripts/reset-anvil.sh`**：

```bash
#!/bin/bash
# 重置 Anvil 到创世块（仅用于 8092，不影响 8082）

echo "🔄 重置 Anvil 到创世块..."

# ⚠️ 检查 8082 是否在运行
if docker ps | grep -q "web3-indexer-app"; then
    echo "⚠️  警告：8082 容器正在运行！"
    echo "请先停止 8082：docker stop web3-indexer-app"
    echo "或者重置会清空 8082 的数据库！"
    read -p "继续吗？(y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# 继续重置...
docker stop web3-demo2-anvil
docker rm web3-demo2-anvil
docker compose -p web3-indexer -f configs/docker/docker-compose.yml up -d anvil
```

### 方案 3: 使用独立的 Anvil 实例

**为 8082 和 8092 使用不同的 Anvil**：

```yaml
# configs/docker/docker-compose.yml

services:
  # 8082 专用 Anvil（持久化）
  anvil-prod:
    image: ghcr.io/foundry-rs/foundry:latest
    command: ["anvil", "--host", "0.0.0.0", "--port", "8545"]
    ports:
      - "8545:8545"

  # 8092 专用 Anvil（临时）
  anvil-dev:
    image: ghcr.io/foundry-rs/foundry:latest
    command: ["anvil", "--host", "0.0.0.0", "--port", "8546", "--anvil.temp"]
    ports:
      - "8546:8546"
```

---

## 📊 端口说明

| 端口 | 用途 | Anvil | 数据库 | 特点 |
|------|------|-------|--------|------|
| **8082** | 正式版（Docker） | 127.0.0.1:8545 | 15432/web3_demo | 持久化，自动重启 |
| **8092** | 开发版（make test-a2） | 127.0.0.1:8545 | 15432/web3_demo | 临时，手动启动 |

**问题**：
- ❌ 两个端口共享同一个 Anvil
- ❌ 重置 Anvil 会影响 8082

---

## 🎯 推荐方案

### 方案 A: 使用独立的 Anvil（推荐）⭐

**修改端口分配**：

```
8082 → Anvil 8545（持久化，不重置）
8092 → Anvil 8546（临时，可重置）
```

**好处**：
- ✅ 互不干扰
- ✅ 8092 可以随意重置
- ✅ 8082 保持稳定

### 方案 B: 重置前先停止 8082

**工作流程**：

```bash
# 1. 停止 8082
docker stop web3-indexer-app

# 2. 重置 Anvil
./scripts/reset-anvil.sh

# 3. 重启 8082
docker start web3-indexer-app

# 4. 启动 8092
make test-a2
```

---

## 🛠️ 快速修复

### 如果 8082 没有数据

```bash
# 1. 检查状态
curl -s http://localhost:8082/api/status | jq '.latest_block, .latest_indexed'

# 2. 如果 latest_indexed = 0，重启容器
docker restart web3-indexer-app

# 3. 等待 5 秒，检查恢复
sleep 5
curl -s http://localhost:8082/api/status | jq '.state, .total_blocks'
```

### 如果 8092 需要重置

```bash
# 1. 先停止 8082（避免影响）
docker stop web3-indexer-app

# 2. 重置 Anvil
./scripts/reset-anvil.sh

# 3. 重启 8082
docker start web3-indexer-app

# 4. 启动 8092
make test-a2
```

---

## ✅ 当前状态

### 8082（正式版）
```
Latest (on Chain): 3303
Latest Indexed:      3302
Total Blocks:        3302
State:               running
Sync Lag:            1
```
✅ **已恢复正常**

### 8092（开发版）
```
Latest (on Chain): ~150
Latest Indexed:      ~150
Total Blocks:        ~150
State:               running
```
✅ **从 0 重新开始**

---

## 🔒 预防措施

### 1. 更新重置脚本

**修改 `scripts/reset-anvil.sh`**，添加检查：

```bash
# 检查 8082 是否在运行
if docker ps | grep -q "web3-indexer-app"; then
    echo "⚠️  警告：8082 正在运行！"
    echo "重置 Anvil 会清空 8082 的数据！"
    read -p "继续吗？(y/N) " -n 1 -r
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi
```

### 2. 使用 Makefile 目标

**添加到 `makefiles/docker.mk`**：

```makefile
.PHONY: reset-anvil-safe

reset-anvil-safe:
	@echo "🔄 安全重置 Anvil..."
	@if docker ps | grep -q "web3-indexer-app"; then \
		echo "⚠️  检测到 8082 正在运行"; \
		echo "🛑 先停止 8082..."; \
		docker stop web3-indexer-app; \
		sleep 2; \
	fi
	@docker stop web3-demo2-anvil 2>/dev/null || true
	@docker rm web3-demo2-anvil 2>/dev/null || true
	@docker compose -p web3-indexer -f configs/docker/docker-compose.yml up -d anvil
	@sleep 3
	@if docker ps -a | grep -q "web3-indexer-app"; then \
		echo "🚀 重启 8082..."; \
		docker start web3-indexer-app; \
	fi
	@echo "✅ Anvil 已重置"
```

---

## 📝 总结

### 问题
- 8082 和 8092 共享同一个 Anvil
- 重置 Anvil 导致 8082 失去连接
- 8082 重启后触发 Nuclear Reset，清空数据库

### 解决方案
1. ✅ **立即修复**：重启 8082 容器
2. ✅ **长期方案**：使用独立的 Anvil 实例
3. ✅ **预防措施**：重置前检查并停止 8082

### 当前状态
- ✅ 8082 已恢复正常（3302 块）
- ✅ 8092 从 0 开始（~150 块）
- ⚠️  两者仍共享同一个 Anvil

---

**最后更新**: 2026-02-20
**状态**: ✅ 已修复
**建议**: 考虑为 8082 和 8092 使用独立的 Anvil 实例
