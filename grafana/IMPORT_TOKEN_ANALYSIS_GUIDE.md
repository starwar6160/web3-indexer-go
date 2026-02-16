# Grafana Token Analysis Dashboard 导入指南

## 🎯 目标

将 "Web3 Token Analysis Dashboard" 导入到 Grafana，实时监控热门代币的转账活动。

---

## 📋 前置条件

1. **Grafana 已运行**（端口 3000）
2. **Prometheus 数据源已配置**
   - Prometheus 正在抓取 `http://localhost:8083/metrics`
   - 可以访问 http://localhost:9090 验证

3. **PostgreSQL 数据源已配置**（用于 Token Analysis Dashboard）
   - 数据库：`web3_sepolia`
   - 用户：`postgres`
   - 密码：`W3b3_Idx_Secur3_2026_Sec`
   - 主机：`web3-testnet-db:5432`（容器内部）或 `localhost:15432`（宿主机）

---

## 🚀 快速导入（5 分钟）

### Step 1: 访问 Grafana

```bash
# 如果 Grafana 未运行，启动它
docker start web3-indexer-grafana

# 访问
open http://localhost:3000
```

### Step 2: 登录

- 用户名: `admin`
- 密码: `admin`（首次登录后修改）

### Step 3: 添加 PostgreSQL 数据源

1. 点击左侧菜单 **"Configuration"** → **"Data sources"**
2. 点击 **"Add data source"**
3. 选择 **"PostgreSQL"**

**配置参数**:
```
Host: web3-testnet-db:5432
Database: web3_sepolia
User: postgres
Password: W3b3_Idx_Secur3_2026_Sec
SSL: Disable
```

4. 点击 **"Save & Test"**，确认显示 "Database Connection OK"

### Step 4: 导入 Dashboard

#### 方法 1: 通过 UI 导入

1. 点击左侧菜单 **"Dashboards"** → **"Import"**
2. 点击 **"Upload JSON file"**
3. 选择文件 `grafana/Token-Analysis-Dashboard.json`
4. 选择 **"PostgreSQL"** 数据源
5. 点击 **"Import"**

#### 方法 2: 通过命令行（更快）

```bash
# 导入 Dashboard（需要 Grafana API Key）
curl -X POST http://localhost:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_GRAFANA_API_KEY" \
  -d @grafana/Token-Analysis-Dashboard.json
```

### Step 5: 验证 Dashboard

导入后，你应该看到：

**顶部 4 个 Stat 面板**:
- 过去 1 小时转账数
- 监控代币数量（应该显示 4）
- 过去 1 小时活跃用户
- 最新索引区块

**中间 2 个图表**:
- 24 小时代币转账趋势（USDC vs DAI）
- 24 小时各代币转账分布（饼图）

**底部 1 个表格**:
- 24 小时代币活动详细统计

---

## 📊 Dashboard 说明

### 面板 1: 过去 1 小时转账数

**SQL**:
```sql
SELECT COUNT(*) FROM transfers
WHERE created_at > NOW() - INTERVAL '1 hour';
```

**意义**: 实时监控 Sepolia 测试网的转账活跃度

### 面板 2: 监控代币数量

**SQL**:
```sql
SELECT COUNT(DISTINCT token_address) FROM transfers;
```

**意义**: 验证代币过滤功能（应该 ≤ 4，因为只监控 USDC, DAI, WETH, UNI）

**注意**: 如果显示 > 4，说明数据库中有旧数据（之前全量索引收集的）。代币过滤只影响新数据。

### 面板 3: 过去 1 小时活跃用户

**SQL**:
```sql
SELECT COUNT(DISTINCT from_addr) FROM transfers
WHERE created_at > NOW() - INTERVAL '1 hour';
```

**意义**: 监控独立发送者数量

### 面板 4: 最新索引区块

**SQL**:
```sql
SELECT COALESCE(MAX(block_number), 0) FROM blocks;
```

**意义**: 验证同步进度

### 面板 5: 24 小时代币转账趋势

**SQL**:
```sql
SELECT created_at, COUNT(*)
FROM transfers
WHERE token_address = '0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238'  -- USDC
  AND created_at > NOW() - INTERVAL '24 hours'
GROUP BY created_at
ORDER BY created_at;
```

**意义**: 对比 USDC vs DAI 的转账活跃度

### 面板 6: 24 小时各代币转账分布

**类型**: Bar Gauge

**意义**: 可视化各代币的转账占比

### 面板 7: 24 小时代币活动详细统计

**SQL**:
```sql
SELECT
  token_symbol,
  token_address,
  COUNT(*) as transfer_count,
  COUNT(DISTINCT from_addr) as unique_senders,
  MAX(block_number) as latest_block,
  MAX(created_at) as last_transfer_time
FROM transfers
WHERE created_at > NOW() - INTERVAL '24 hours'
GROUP BY token_address
ORDER BY transfer_count DESC;
```

**字段**:
- `token_symbol`: 代币符号（USDC, DAI, WETH, UNI）
- `transfer_count`: 转账次数
- `unique_senders`: 唯一发送者数量
- `latest_block`: 最新区块
- `last_transfer_time`: 最后转账时间

---

## 🎨 自定义 Dashboard

### 修改刷新频率

默认：10 秒

1. 点击顶部 **"Refresh interval"**
2. 选择 **"5s"**, **"30s"**, 或其他值

### 修改时间范围

默认：过去 24 小时

1. 点击右上角时间选择器
2. 选择 **"Last 1 hour"**, **"Last 7 days"**, 或自定义

### 添加新的代币

1. 点击面板右上角 **"..."** → **"Edit"**
2. 修改 SQL，添加新的 `token_address`
3. 点击 **"Save"**

### 导出 Dashboard

1. 点击顶部 **"Share"** 图标
2. 选择 **"Export"** → **"Save to file"**

---

## 🔧 故障排查

### 问题 1: 数据库连接失败

**错误**: "Database Connection OK" 显示红色

**解决方案**:
```bash
# 测试数据库连接
docker exec web3-testnet-db psql -U postgres -d web3_sepolia -c "SELECT 1;"

# 检查 Grafana 容器网络
docker network inspect web3-testnet_web3-network

# 确保 Grafana 和 db 在同一网络
docker inspect web3-indexer-grafana | grep NetworkMode
```

### 问题 2: 面板显示 "No Data"

**原因**: 数据库中还没有新的热门代币转账（系统刚启动）

**解决方案**:
- 等待 10-15 分钟，让系统同步一些最新区块
- 检查同步日志: `docker logs web3-debug-app | grep "Starting from"`

### 问题 3: "监控代币数量" 显示 > 4

**原因**: 数据库中有旧数据（之前全量索引收集的）

**解决方案**:
- 这是正常的！代币过滤只影响新索引的数据
- 如果想清空旧数据，运行:
  ```bash
  docker exec web3-testnet-db psql -U postgres -d web3_sepolia \
    -c "TRUNCATE TABLE blocks, transfers CASCADE;"
  ```
- 然后重启容器: `docker restart web3-debug-app`

---

## 📱 共享 Dashboard

### 公开链接

1. 点击顶部 **"Share"** 图标
2. 启用 **"Public dashboard"**
3. 复制链接分享给他人

### 嵌入到网站

```html
<iframe
  src="http://localhost:3000/d/token-analysis/web3-token-analysis-dashboard?orgId=1&refresh=10s&kiosk"
  width="100%"
  height="1000"
  frameborder="0">
</iframe>
```

### 导出为 PDF

1. 点击顶部 **"Share"** 图标
2. 选择 **"Export"** → **"PDF"**

---

## 🎯 最佳实践

### 1. 设置告警

当某个代币的转账数异常时，发送通知：

1. 点击面板 → **"Alert"** 图标
2. 设置条件（如：过去 1 小时 USDC 转账数 < 10）
3. 选择通知方式（Email, Slack, Webhook）

### 2. 定期备份数据库

```bash
# 每天备份一次
docker exec web3-testnet-db pg_dump -U postgres web3_sepolia \
  > backup_$(date +%Y%m%d).sql
```

### 3. 监控 Grafana 性能

- 确保刷新频率不要太高（建议 10-30 秒）
- 避免运行过于复杂的 SQL 查询
- 定期清理旧数据（保留最近 30 天）

---

## 📚 参考资源

- **Grafana 官方文档**: https://grafana.com/docs/
- **PostgreSQL 数据源**: https://grafana.com/docs/grafana/latest/datasources/postgres/
- **Dashboard JSON 模型**: https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/

---

**最后更新**: 2026-02-16
**维护者**: Claude Code
