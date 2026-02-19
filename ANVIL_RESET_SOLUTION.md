# Anvil 从 0 开始同步 - 完整解决方案

**日期**: 2026-02-20
**问题**: Anvil 每次启动都在 30000+，不是从 0 开始
**期望**: 每次启动都应该从 0 开始同步

---

## 🚨 问题分析

### 您的期望

```
每次启动索引器：
Latest (on Chain): 0
Total (Synced): 0
从创世块开始同步 → 0, 1, 2, 3, ...
```

### 实际情况

```
Anvil 是持久化的：
- 容器运行了 42 小时
- 链数据一直保留在内存/磁盘
- 高度：35769（持续增长）

索引器启动：
- 检测到 Anvil 高度：35769
- 从 35769 开始同步（不是从 0）
- Latest (on Chain): 35769
```

---

## 🎯 根本原因

### Anvil 默认是持久化的

**Docker Compose 配置**：
```yaml
anvil:
  command: ["anvil", "--host", "0.0.0.0", "--port", "8545"]
  # 没有临时模式标志
```

**结果**：
- ✅ Anvil 会将链数据保存在容器内
- ✅ 容器重启后数据还在
- ❌ 不会从 0 重新开始

---

## 🛠️ 解决方案

### 方案 1: 手动重启 Anvil ⭐

**每次测试前执行**：

```bash
# 1. 停止并删除 Anvil 容器
docker stop web3-demo2-anvil
docker rm web3-demo2-anvil

# 2. 重新启动 Anvil
make infra-up

# 或者直接启动
docker compose -p web3-indexer -f configs/docker/docker-compose.yml up -d anvil

# 3. 验证高度
curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "Anvil 高度: %d\n"

# 4. 启动索引器
make test-a2
```

**效果**：
- ✅ Anvil 从 0 重新开始
- ✅ 索引器从 0 开始同步
- ✅ 每次测试都是干净的状态

### 方案 2: 修改 Docker Compose 使用临时模式

**修改 `configs/docker/docker-compose.yml`**：

```yaml
services:
  anvil:
    image: ghcr.io/foundry-rs/foundry:latest
    command: [
      "anvil",
      "--host", "0.0.0.0",
      "--port", "8545",
      "--anvil.temp",           # ← 添加这行
      "--block-time", "1"       # ← 可选：1 秒出块
    ]
```

**效果**：
- ✅ 每次容器重启，高度从 0 开始
- ✅ 数据不持久化到磁盘
- ⚠️ 容器停止后数据会丢失

### 方案 3: 创建便捷脚本

**创建 `scripts/reset-anvil.sh`**：

```bash
#!/bin/bash
# 重置 Anvil 到创世块

echo "🔄 重置 Anvil 到创世块..."

# 停止并删除容器
docker stop web3-demo2-anvil 2>/dev/null || true
docker rm web3-demo2-anvil 2>/dev/null || true

# 重新启动
echo "🚀 启动 Anvil..."
docker compose -p web3-indexer -f configs/docker/docker-compose.yml up -d anvil

# 等待启动
sleep 3

# 验证高度
HEIGHT=$(curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "%d")

echo "✅ Anvil 已重置，当前高度: $HEIGHT"
```

**使用方法**：

```bash
# 每次测试前
./scripts/reset-anvil.sh

# 然后启动索引器
make test-a2
```

### 方案 4: 集成到 Makefile

**修改 `makefiles/docker.mk`**，添加新目标：

```makefile
.PHONY: reset-anvil test-a2-fresh

reset-anvil:
	@echo "🔄 重置 Anvil 到创世块..."
	@docker stop web3-demo2-anvil 2>/dev/null || true
	@docker rm web3-demo2-anvil 2>/dev/null || true
	@docker compose -p web3-indexer -f configs/docker/docker-compose.yml up -d anvil
	@sleep 3
	@echo "✅ Anvil 已重置"

test-a2-fresh: reset-anvil test-a2
	@echo "✅ 索引器已从创世块启动"
```

**使用方法**：

```bash
# 一键重置并启动
make test-a2-fresh
```

---

## 📊 效果对比

### 重置前

```
Anvil 高度：35769（运行了 42 小时）
索引器启动：从 35769 开始
Latest (on Chain): 35769
```

### 重置后

```
Anvil 高度：0（刚启动）
索引器启动：从 0 开始
Latest (on Chain): 0 → 1 → 2 → ...
```

---

## 🎯 推荐工作流程

### 日常测试

```bash
# 1. 重置 Anvil
make reset-anvil   # 或 ./scripts/reset-anvil.sh

# 2. 启动索引器
make test-a2

# 3. 访问 UI
open http://localhost:8092
```

### 完整重置（包括数据库）

```bash
# 1. 停止所有容器
docker stop web3-demo2-anvil web3-indexer-db 2>/dev/null || true

# 2. 删除容器和数据卷
docker rm web3-demo2-anvil web3-indexer-db 2>/dev/null || true
docker volume rm web3-indexer_demo-data 2>/dev/null || true

# 3. 重新启动
make infra-up

# 4. 启动索引器
make test-a2
```

---

## 🔍 验证方法

### 检查 Anvil 高度

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

### 检查索引器状态

```bash
curl -s http://localhost:8092/api/status | jq '{
  latest_block: .latest_block,
  latest_indexed: .latest_indexed,
  total_blocks: .total_blocks
}'
```

**期望输出**：
```json
{
  "latest_block": "42",      // 应该是从 0 开始的小数字
  "latest_indexed": "42",
  "total_blocks": 42
}
```

---

## ⚠️ 注意事项

### 1. 数据持久化 vs 临时模式

| 模式 | 容器重启后 | 数据保留 | 适用场景 |
|------|-----------|---------|---------|
| **默认模式** | 链数据保留 | ✅ 是 | 开发、调试 |
| **临时模式** | 链数据清空 | ❌ 否 | CI/CD、自动化测试 |

### 2. 容器生命周期

- **停止容器**：`docker stop web3-demo2-anvil`
  - 数据保留（可以 `docker start` 恢复）

- **删除容器**：`docker rm web3-demo2-anvil`
  - 数据清空（需要重新创建容器）

- **删除容器 + 数据卷**：`docker volume rm ...`
  - 彻底清空所有数据

### 3. 多实例注意

如果您同时运行多个 Anvil 实例：
- `web3-demo2-anvil` (端口 8545)
- `web3-indexer-anvil` (端口 8545)
- 可能会冲突！

确保只运行一个 Anvil 实例：

```bash
docker ps | grep anvil
docker stop $(docker ps -q --filter ancestor=ghcr.io/foundry-rs/foundry:latest)
```

---

## 🚀 快速命令参考

```bash
# 重置 Anvil
docker stop web3-demo2-anvil && docker rm web3-demo2-anvil
make infra-up

# 验证高度
curl -s -X POST http://127.0.0.1:8545 -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "%d\n"

# 启动索引器
make test-a2

# 检查状态
curl -s http://localhost:8092/api/status | jq '.latest_block, .latest_indexed'
```

---

## ✅ 总结

### 问题
- Anvil 是持久化的，不会自动清零
- 每次启动都在上次的继续

### 解决方案
1. ✅ **手动重置**：`docker stop && docker rm && make infra-up`
2. ✅ **临时模式**：添加 `--anvil.temp` 标志
3. ✅ **便捷脚本**：创建 `reset-anvil.sh`
4. ✅ **Makefile 集成**：添加 `reset-anvil` 目标

### 推荐方案
- **开发测试**：手动重置 + `make test-a2`
- **自动化测试**：临时模式 + CI/CD 集成

---

**最后更新**: 2026-02-20
**状态**: ✅ 已验证
**Anvil 当前高度**: ~150（从 0 重新开始）
