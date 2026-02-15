# Grafana Dashboard - 导入指南

## 📊 概述

这个 Dashboard 专为 **Web3 Indexer 生产监控** 设计，体现"6 个 9 持久性"的工程标准。

---

## 🎯 包含的面板（10 个）

### 第一行：核心状态（Top Row）

1. **Sync Lag (Blocks Behind)**
   - 类型：Gauge
   - PromQL: `indexer_sync_lag_blocks`
   - 阈值：< 1000 绿色，≥ 1000 红色
   - 修复后显示：~136（正确！）

2. **Real-time TPS**
   - 类型：Sparkline 趋势图
   - PromQL: `rate(indexer_transfers_total[1m])`
   - 显示：~7.75 TPS

3. **E2E Latency (Seconds)**
   - 类型：Stat
   - PromQL: `indexer_sync_lag_blocks * 12`
   - 阈值：< 10分钟 绿色，10-60分钟 黄色，> 60分钟 红色
   - 显示：~1632 秒（27 分钟）

4. **RPC Health**
   - 类型：State
   - PromQL: `indexer_rpc_healthy_nodes`
   - 显示：2/2 节点健康

### 第二行：性能监控（Performance）

5. **RPC Consumption (Rate Limit Monitor)**
   - 类型：Bar Chart
   - PromQL: `rate(indexer_rpc_requests_total[1m])`
   - 目的：证明 QPS 控制有效
   - 显示：~1 req/s（保守配置）

6. **Block Height Tracking**
   - 类型：Line Chart (双线)
   - PromQL:
     - `indexer_current_chain_height` (链头)
     - `indexer_current_sync_height` (已同步)
   - 目的：可视化同步进度

7. **Database Performance (SQL Latency)**
   - 类型：Line Chart (p95/p99)
   - PromQL:
     - `histogram_quantile(0.95, indexer_db_query_duration_seconds_bucket)`
     - `histogram_quantile(0.99, indexer_db_query_duration_seconds_bucket)`
   - 目的：证明 PostgreSQL 高性能

### 第三行：吞吐量（Throughput）

8. **Processing Throughput**
   - 类型：Line Chart (双线)
   - PromQL:
     - `rate(indexer_blocks_processed_total[1m])`
     - `rate(indexer_transfers_processed_total[1m])`
   - 目的：展示系统吞吐量

9. **Sequencer Buffer**
   - 类型：Gauge
   - PromQL: `indexer_sequencer_buffer_size`
   - 目的：监控缓冲区使用

10. **Self-Healing Count**
    - 类型：Stat
    - PromQL: `indexer_self_healing_count`
    - 目的：追踪自愈事件

---

## 🚀 导入步骤

### 方法 1：通过 Grafana UI（推荐）

1. 打开 Grafana：`http://localhost:3000`（或你的 Cloudflare Tunnel URL）

2. 登录（默认：admin/admin）

3. 点击左侧菜单 **"+"** → **"Import"**

4. 选择 **"Upload JSON file"**

5. 上传文件：`grafana/Web3-Indexer-Dashboard.json`

6. 选择 Prometheus 数据源

7. 点击 **"Import"**

### 方法 2：通过 Grafana API（自动化）

```bash
# 导入 Dashboard
curl -X POST \
  http://localhost:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_GRAFANA_API_KEY" \
  -d @grafana/Web3-Indexer-Dashboard.json
```

### 方法 3：使用 Docker Volume（持久化）

```bash
# 复制到 Grafana 容器
docker cp grafana/Web3-Indexer-Dashboard.json \
  grafana:/etc/grafana/provisioning/dashboards/

# 重启 Grafana
docker restart grafana
```

---

## 🎨 自定义建议

### 调整刷新频率

默认：5 秒

```json
"refresh": "5s"
```

生产环境建议：
- 实时监控：5s - 10s
- 长期趋势：30s - 1m

### 调整时间范围

默认：`now-1h` to `now`

```json
"time": {
  "from": "now-6h",
  "to": "now"
}
```

建议：
- 开发环境：1h
- 测试网：6h
- 主网：24h - 7d

### 调整阈值

**Sync Lag 阈值**（第 1 行第 1 列）：

```json
"thresholds": {
  "mode": "absolute",
  "steps": [
    {"color": "green", "value": null},
    {"color": "yellow", "value": 1000},  // 1000 块
    {"color": "red", "value": 5000}      // 5000 块
  ]
}
```

**E2E Latency 阈值**（第 1 行第 3 列）：

```json
"thresholds": {
  "mode": "absolute",
  "steps": [
    {"color": "green", "value": null},
    {"color": "yellow", "value": 600},   // 10 分钟
    {"color": "red", "value": 3600}      // 60 分钟
  ]
}
```

**RPC QPS 阈值**（第 2 行第 1 列）：

```json
"thresholds": {
  "mode": "absolute",
  "steps": [
    {"color": "green", "value": null},
    {"color": "yellow", "value": 2},     // 2 req/s
    {"color": "red", "value": 5}         // 5 req/s
  ]
}
```

---

## 💡 面试话术（Dashboard 设计）

当面试官询问时：

"这个 Dashboard 体现了**生产级监控**的三个核心原则：

1. **可操作性 (Actionable)**
   - Sync Lag 告诉我们需要加快同步
   - RPC Health 告诉我们需要检查节点
   - Self-Healing Count 告诉我们系统稳定性

2. **可解释性 (Explainable)**
   - E2E Latency 解释为：同步滞后 × 12秒/块
   - Real-time TPS 使用 rate() 函数计算实时速率
   - 每个指标都有清晰的 PromQL 和单位

3. **专业度 (Professional)**
   - RPC Consumption 证明我们控制了 QPS（避免滥用测试网额度）
   - DB Performance 证明 PostgreSQL 在高并发下保持高性能
   - p95/p99 延迟展示符合 SRE 最佳实践

**关键亮点**：
- 修复前 Sync Lag 显示 1000 万块（误导性）
- 修复后显示 136 块（准确）
- 这个细节体现了对指标的精确理解。"

---

## 📱 移动端优化

如果在手机上查看觉得拥挤，可以调整布局：

### 选项 1：减少列数

将第 1 行从 4 列改为 2 列：

```json
// 面板 1 (Sync Lag)
"gridPos": {"h": 4, "w": 12, "x": 0, "y": 0}

// 面板 2 (Real-time TPS)
"gridPos": {"h": 8, "w": 12, "x": 0, "y": 4}
```

### 选项 2：创建移动端专用 Dashboard

复制 JSON 文件，调整：
- `gridPos.w` (宽度): 8 → 24 (全宽)
- `graphMode`: "area" → "none" (减少图表)
- `legend.displayMode`: "list" → "table" (更紧凑)

---

## 🔍 故障排查

### 问题 1：Metrics 不显示

**症状**：Dashboard 面板显示 "No data"

**原因**：Prometheus 数据源配置错误

**解决**：
1. 检查 Prometheus 是否运行：`docker ps | grep prometheus`
2. 检查 metrics 端点：`curl http://localhost:8081/metrics`
3. 验证 PromQL：在 Grafana → Explore 中测试查询

### 问题 2：Sync Lag 仍然错误

**症状**：Sync Lag 显示巨大的数字

**原因**：代码未重新构建

**解决**：
```bash
# 查看当前提交
git log --oneline -1

# 应该看到：fix(monitoring): correct sync lag calculation
# 提交 hash: 43b35cb

# 如果不是，拉取最新代码
git pull origin main

# 重新构建
docker compose -f docker-compose.testnet.yml \
  --env-file .env.testnet.local \
  -p web3-testnet build --no-cache sepolia-indexer

# 重启
docker compose -f docker-compose.testnet.yml \
  --env-file .env.testnet.local \
  -p web3-testnet up -d --force-recreate sepolia-indexer
```

### 问题 3：Real-time TPS 为 0

**症状**：TPS 始终显示 0

**原因**：Prometheus `rate()` 函数需要至少 2 个数据点

**解决**：
- 等待 10-20 秒让 Prometheus 抓取 2 次 metrics
- 或使用 `irate()`（瞬时速率）替代 `rate()`

---

## 🎯 下一步优化

1. **添加告警 (Alerting)**
   - Sync Lag > 5000
   - RPC Health < 2
   - E2E Latency > 1 小时

2. **添加 Annotation**
   - 标记重启事件
   - 标记 RPC 节点切换
   - 标记自愈事件

3. **创建变量 (Variables)**
   - Chain (Sepolia, Mainnet)
   - RPC Provider (QuickNode, Infura)

---

**文档版本**：v1.0
**Dashboard 版本**：1
**最后更新**：2026-02-15
**维护者**：追求 6 个 9 持久性的资深后端
