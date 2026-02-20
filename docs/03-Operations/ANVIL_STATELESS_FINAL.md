# Anvil 无状态配置 - 最终方案

**日期**: 2026-02-20
**目标**: 8082 和 8092 基于 Anvil，尽量无状态
**方案**: Nuclear Reset + 手动清理 Anvil

---

## ✅ 最终配置

### Anvil 配置（稳定版）

**文件**: `configs/docker/docker-compose.yml`

```yaml
anvil:
  image: ghcr.io/foundry-rs/foundry:latest
  container_name: ${COMPOSE_PROJECT_NAME}-anvil
  profiles: ["demo"]
  network_mode: host
  entrypoint: ["anvil"]
  command: [
    "--host", "0.0.0.0",
    "--port", "8545",
    "--block-time", "1"
  ]
  deploy:
    resources:
      limits:
        memory: 2G
  tmpfs:
    - /home/foundry/.foundry/anvil/tmp:rw,size=100M,noexec,nosuid,nodev
  restart: always
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8545"]
    interval: 30s
    timeout: 10s
    retries: 3
    start_period: 5s
```

**说明**：
- ✅ 使用稳定的 Anvil 配置（无临时模式标志）
- ✅ tmpfs 确保数据不持久化到宿主机磁盘
- ✅ 容器重启后数据清空（在容器内）

---

## 🎯 无状态实现

### 1. 索引器 Nuclear Reset（自动）

**`AnvilStrategy.OnStartup()`**：

```go
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
- ✅ 每次启动自动清空数据库
- ✅ 内存状态归零
- ✅ 从块 0 开始同步

### 2. Anvil 清理（手动）

**脚本**: `scripts/clean-state.sh`

```bash
docker stop web3-indexer-anvil
docker rm web3-indexer-anvil
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d anvil
```

**效果**：
- ✅ Anvil 容器重启
- ✅ 高度从 0 开始
- ✅ 数据不保留

---

## 🚀 使用方法

### 完全清理（推荐）

```bash
# 一键清理所有状态
make clean-state
```

**效果**：
- Anvil 重启 → 高度从 0 开始
- 数据库清空
- 8082 重启 → 从 0 开始同步

### 仅重启索引器

```bash
# 8082
docker restart web3-indexer-app

# 8092
lsof -ti:8092 | xargs kill -9 2>/dev/null || true
make test-a2
```

**效果**：
- Nuclear Reset 自动执行
- 数据库清空
- 从当前 Anvil 高度开始同步

### 仅重启 Anvil

```bash
docker stop web3-indexer-anvil
docker rm web3-indexer-anvil
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d anvil
```

**效果**：
- Anvil 高度从 0 开始
- 需要重启索引器才能同步

---

## 📊 验证

### Anvil 高度

```bash
$ curl -s http://127.0.0.1:8545 ...
Anvil 高度: 5  # ✅ 从 0 开始
```

### 索引器状态

```bash
$ curl -s http://localhost:8082/api/status
{
  "latest_block": "5",
  "latest_indexed": "5",
  "total_blocks": 5
}
```

---

## ⚠️ 注意事项

### Anvil 持久化

**当前配置**：
- ✅ 数据在容器内 tmpfs
- ✅ 容器删除后数据丢失
- ❌ 容器重启（restart）数据保留

**无状态方案**：
- **索引器**：Nuclear Reset（自动）
- **Anvil**：删除容器重建（手动）

### 使用建议

**开发测试**：
```bash
# 每次测试前
make clean-state
make test-a2
```

**日常使用**：
```bash
# 仅重启索引器（Nuclear Reset）
docker restart web3-indexer-app
```

---

## ✅ 总结

### 无状态实现

| 组件 | 无状态方式 | 触发方式 |
|------|-----------|---------|
| **索引器** | Nuclear Reset | 自动（每次启动） |
| **Anvil** | 删除容器重建 | 手动（make clean-state） |

### 效果

- ✅ **8082**：重启后 Nuclear Reset，从 0 开始
- ✅ **8092**：每次运行 Nuclear Reset，从 0 开始
- ✅ **Anvil**：手动清理后从 0 开始

### 使用

```bash
# 完全清理
make clean-state

# 启动 8082
make a2

# 启动 8092
make test-a2
```

---

**最后更新**: 2026-02-20
**状态**: ✅ 稳定配置
**无状态**: ✅ 已实现（索引器自动 + Anvil 手动）
