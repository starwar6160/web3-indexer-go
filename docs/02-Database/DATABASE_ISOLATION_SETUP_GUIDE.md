# 🎯 数据库物理隔离 - 完成报告

**日期**: 2026-02-16
**架构**: 三环境物理隔离

---

## ✅ 完成的工作

### 1. 创建了三个独立的 PostgreSQL 数据库

| 数据库名称 | 用途 | 端口 | 数据状态 |
|-----------|------|------|----------|
| `web3_indexer_demo1` | 8081 (线上监控版) | 8081 | 1 block, 0 transfers |
| `web3_indexer_debug` | 8083 (调试过滤版) | 8083 | 0 block, 0 transfers |
| `web3_sepolia` | 旧数据库（可废弃） | - | 1 block, 0 transfers |

### 2. 更新了环境配置

**`.env.testnet`**:
```bash
DATABASE_URL=postgres://postgres:W3b3_Idx_Secur3_2026_Sec@sepolia-db:5432/web3_indexer_demo1?sslmode=disable
```

**`docker-compose.debug.yml`**:
```yaml
DATABASE_URL=postgres://postgres:W3b3_Idx_Secur3_2026_Sec@web3-testnet-db:5432/web3_indexer_debug?sslmode=disable
```

### 3. 添加了数据库管理 Makefile 命令

```bash
make db-list              # 查看所有数据库统计
make db-clean-debug       # 清空 Debug 数据库（保留结构）
make db-reset-debug       # 重置 Debug 数据库（删除并重建）
make db-sync-schema       # 同步 Schema（Demo1 → Debug）
make db-backup-demo1      # 备份 Demo1 数据
make db-restore-demo1     # 恢复 Demo1 数据（从最新备份）
```

### 4. 容器已重启并连接到正确的数据库

- ✅ **8081 (testnet)**: 连接到 `web3_indexer_demo1`
- ✅ **8083 (debug)**: 连接到 `web3_indexer_debug`

---

## 🔧 Grafana 数据源配置（手动）

由于 API 配置较复杂，建议手动配置：

### Step 1: 访问 Grafana

```
http://localhost:4000
```

**登录**: admin / W3b3_Idx_Secur3_2026_Sec

### Step 2: 创建 Demo1 数据源

1. **Configuration** (⚙️) → **Data sources**
2. **Add data source** → 搜索 "PostgreSQL"
3. 配置：
   - **Name**: `PostgreSQL-Demo1`
   - **Host**: `localhost:15432` (或 `web3-testnet-db:5432`)
   - **Database**: `web3_indexer_demo1`
   - **User**: `postgres`
   - **Password**: `W3b3_Idx_Secur3_2026_Sec`
   - **SSL Mode**: `disable`
4. **Save & Test**

### Step 3: 创建 Debug 数据源

重复 Step 2，但修改：
   - **Name**: `PostgreSQL-Debug`
   - **Database**: `web3_indexer_debug`
   - **UID**: `postgres_debug_ds`

### Step 4: 更新 Dashboard 面板

对于每个需要切换数据源的面板：

1. 打开 Dashboard 编辑模式
2. 点击面板右上角 **...** → **Edit**
3. 在 **Query** 设置中，将 **Data source** 改为对应的数据源：
   - **8081 Dashboard**: 使用 `PostgreSQL-Demo1`
   - **8083 Dashboard**: 使用 `PostgreSQL-Debug`
4. **Save** 保存修改

---

## 📊 验证隔离效果

### 测试 1: 数据库隔离

```bash
make db-list
```

**期望输出**:
- Demo1: 显示 1 block
- Debug: 显示 0 blocks

### 测试 2: 容器连接

```bash
docker logs web3-testnet-app 2>&1 | grep -E "(DATABASE_URL|Database)"
docker logs web3-debug-app 2>&1 | grep -E "(DATABASE_URL|Token filtering)"
```

**期望输出**:
- 8081: 连接到 `web3_indexer_demo1`
- 8083: 连接到 `web3_indexer_debug` + 显示 "Token filtering enabled"

### 测试 3: 数据隔离

在 Debug 数据库中插入一条测试记录：

```bash
docker exec web3-testnet-db psql -U postgres -d web3_indexer_debug -c \
  "INSERT INTO blocks (number, hash, timestamp) VALUES (999999, '0xtest', 1234567890);"
```

然后检查 Demo1 数据库：

```bash
docker exec web3-testnet-db psql -U postgres -d web3_indexer_demo1 -c \
  "SELECT * FROM blocks WHERE number = 999999;"
```

**期望结果**: Demo1 中没有这条记录（验证隔离成功）

---

## 🎉 架构优势

### 隔离前后对比

| 维度 | 隔离前 | 隔离后 |
|------|--------|--------|
| **数据库** | 8081 和 8083 共用 `web3_sepolia` | 8081 → `web3_indexer_demo1`<br>8083 → `web3_indexer_debug` |
| **数据污染** | Debug 环境的 `TRUNCATE` 会影响 Demo1 | 完全独立，互不影响 |
| **Grafana** | 需手动切换数据源 | 可配置多个数据源，Dashboard 绑定 |
| **运维** | 风险高，操作需谨慎 | 安全，Debug 环境可随意实验 |

### 运维自由度

**Demo1 (8081)**:
- ⭐⭐⭐⭐⭐⭐ 持久性要求
- ✅ 只读展示，避免误操作
- ✅ 定期备份

**Debug (8083)**:
- ⭐ 持久性要求
- ✅ 可随意 `TRUNCATE`、`DROP`
- ✅ 测试代币过滤功能
- ✅ 快速迭代实验

---

## 📝 下一步建议

### 1. 添加定时备份（可选）

```bash
# 添加到 crontab
0 2 * * * cd /home/ubuntu/zwCode/web3-indexer-go && make db-backup-demo1
```

### 2. 配置 Grafana 自动化（高级）

使用 Grafana Terraform Provider 或 Ansible 自动化数据源配置。

### 3. 监控数据库大小

```bash
watch -n 60 'make db-list'
```

定期检查各数据库的大小，避免磁盘空间耗尽。

---

**状态**: ✅ **数据库物理隔离完成**
**下一步**: 配置 Grafana 多数据源，实现 Dashboard 层面的视觉隔离

---

**创建时间**: 2026-02-16 00:45 JST
**维护者**: Claude Code
