# Grafana Token Metrics Dashboard 导入指南

## 🎯 目标

将 "Web3 Token Metrics Dashboard" 导入到 Grafana，实时监控热门代币的转账活动和 RPC 额度使用率。

---

## 📋 前置条件

1. **Grafana 已运行**（端口 3000）
2. **Prometheus 数据源已配置**
   - Prometheus 正在抓取 `http://localhost:8083/metrics`
3. **Indexer 正在运行**（8083 端口）
   - 已处理一些 Transfer 事件

---

## 🚀 快速导入（5 分钟）

### Step 1: 访问 Grafana

```bash
# 确保 Grafana 运行
docker start web3-indexer-grafana

# 访问
open http://localhost:3000
```

**登录**:
- 用户名: `admin`
- 密码: `admin`（首次登录后修改）

### Step 2: 确认 Prometheus 数据源

1. 点击左侧菜单 **"Configuration"** (齿轮图标) → **"Data sources"**
2. 检查是否有 **"Prometheus"** 数据源
3. 如果没有，点击 **"Add data source"**
4. 选择 **"Prometheus"**

**配置参数**:
```
Name: Prometheus
URL: http://prometheus:9090
Access: Server (default)
```

5. 点击 **"Save & Test"**，确认显示 "Data source is working"

### Step 3: 导入 Dashboard

#### 方法 1: 通过 UI 导入（推荐）

1. 点击左侧菜单 **"Dashboards"** → **"Import"**
2. 点击 **"Upload JSON file"**
3. 选择文件 `grafana/Token-Metrics-Dashboard.json`
4. 选择 **"Prometheus"** 数据源
5. 点击 **"Import"**

#### 方法 2: 复制粘贴（更快）

1. 打开 `grafana/Token-Metrics-Dashboard.json`
2. 复制全部内容（Ctrl+A, Ctrl+C）
3. 在 Grafana Import 页面，点击 **"Paste or drag JSON here"**
4. 粘贴 JSON 内容
5. 点击 **"Load"**
6. 选择 **"Prometheus"** 数据源
7. 点击 **"Import"**

---

## 📊 Dashboard 说明

### 面板概览

| 面板编号 | 面板名称 | 类型 | 描述 |
|---------|---------|------|------|
| 1 | USDC 过去 1 小时流水 | Stat | 显示 USDC 转账总量（美元） |
| 2 | 过去 1 小时总转账数 | Stat | 显示所有代币的转账次数 |
| 3 | 24 小时代币转账趋势 | Time Series | USDC/DAI/WETH/UNI 转账量趋势图 |
| 4 | 四大热门代币转账次数占比 | Pie Chart | 饼图显示各代币的转账占比 |
| 5 | 实时转账速率（TPS） | Time Series | 每秒转账数（按代币） |
| 6 | 🛡️ RPC QUOTA GUARD (DAILY) | Gauge | RPC 额度使用率仪表盘 |
| 7 | 24 小时代币活动详细统计 | Table | 各代币的转账统计表格 |

### PromQL 查询详解

#### 面板 1: USDC 过去 1 小时流水
```promql
sum(increase(indexer_token_transfer_volume_total{symbol="USDC"}[1h]))
```

#### 面板 2: 过去 1 小时总转账数
```promql
sum(increase(indexer_token_transfer_count_total[1h]))
```

#### 面板 3: 24 小时代币转账趋势
```promql
sum by(symbol) (increase(indexer_token_transfer_volume_total{symbol="USDC"}[24h]))
sum by(symbol) (increase(indexer_token_transfer_volume_total{symbol="DAI"}[24h]))
sum by(symbol) (increase(indexer_token_transfer_volume_total{symbol="WETH"}[24h]))
sum by(symbol) (increase(indexer_token_transfer_volume_total{symbol="UNI"}[24h]))
```

#### 面板 4: 四大热门代币转账次数占比
```promql
sum by(symbol) (increase(indexer_token_transfer_count_total[24h]))
```

#### 面板 5: 实时转账速率（TPS）
```promql
sum by(symbol) (rate(indexer_token_transfer_count_total[5m]))
```

#### 面板 6: RPC 额度仪表盘
```promql
rpc_quota_usage_percent
```

**颜色阈值**:
- 0-70%: 绿色（安全）
- 70-90%: 黄色（警告）
- >90%: 红色（临界）

#### 面板 7: 24 小时代币活动详细统计
```promql
sum by(symbol) (increase(indexer_token_transfer_count_total[24h]))
```

---

## 🎨 自定义 Dashboard

### 修改刷新频率

默认：10 秒

1. 点击顶部 **"Refresh interval"**
2. 选择：
   - `5s` （更频繁）
   - `30s` （更保守）
   - `1m` （每分钟）

### 修改时间范围

默认：过去 24 小时

1. 点击右上角时间选择器
2. 选择：
   - `Last 1 hour` （1 小时）
   - `Last 6 hours` （6 小时）
   - `Last 7 days` （7 天）
   - `Custom` （自定义）

### 添加新的代币

如果需要监控更多代币：

1. 修改 `internal/engine/processor_transfer.go` 中的 `getTokenSymbol` 函数
2. 添加新的代币地址映射
3. 重新编译并重启容器

---

## 🔍 验证清单

导入后，检查以下内容：

- [ ] Dashboard 成功导入
- [ ] 面板 1 显示 USDC 流水（即使为 0 也正常）
- [ ] 面板 2 显示总转账数
- [ ] 面板 6 显示 RPC 额度使用率（应该是很低的值）
- [ ] 所有面板没有错误（红色感叹号）

---

## 🐛 故障排查

### 问题 1: 所有面板显示 "No Data"

**原因**:
1. Prometheus 没有抓取到指标
2. Indexer 还没有处理 Transfer 事件
3. 数据源配置错误

**解决方法**:

```bash
# 1. 检查 Prometheus 是否正在抓取
curl http://localhost:9090/api/v1/targets

# 2. 检查指标是否存在
curl http://localhost:8083/metrics | grep indexer_token_transfer

# 3. 检查 Indexer 是否在运行
docker logs web3-debug-app | grep "System Operational"

# 4. 等待 10-15 分钟，让系统处理一些新的区块
```

### 问题 2: RPC 额度仪表盘显示 "N/A"

**原因**: Prometheus 指标未暴露

**解决方法**:

```bash
# 检查额度指标
curl http://localhost:8083/metrics | grep rpc_quota

# 期望输出：
# rpc_quota_status 0
# rpc_quota_usage_percent 0.0033
```

### 问题 3: 面板显示错误信息

**原因**: PromQL 查询语法错误

**解决方法**:

1. 点击面板右上角的 **"..."** → **"Edit"**
2. 检查 PromQL 查询是否正确
3. 点击 **"Refresh"** 验证

---

## 🎯 预期效果

### 正常运行后

**顶部（2 个 Stat 面板）**:
```
USDC 过去 1 小时流水: $1,234.56
过去 1 小时总转账数:   42
```

**中部（左侧 Time Series + 右侧 Pie Chart）**:
```
24 小时代币转账趋势（折线图）
  - USDC:  蓝色线
  - DAI:   黄色线
  - WETH:  紫色线
  - UNI:   粉色线

四大热门代币转账次数占比（饼图）
  - USDC:  45%
  - DAI:   30%
  - WETH:  15%
  - UNI:   10%
```

**底部（左侧 TPS + 右侧 Gauge）**:
```
实时转账速率（TPS）:
  - USDC:  0.5 TPS
  - DAI:   0.3 TPS
  - WETH:  0.1 TPS

🛡️ RPC QUOTA GUARD (DAILY): 15% (绿色)
```

**最底部（详细统计表格）**:
```
代币符号 | 转账次数
---------|----------
USDC     | 150
DAI      | 100
WETH     | 50
UNI      | 35
Other    | 200
```

---

## 📱 共享 Dashboard

### 公开链接

1. 点击顶部 **"Share"** 图标
2. 启用 **"Public dashboard"**
3. 复制链接分享给他人

### 嵌入到网站

```html
<iframe
  src="http://localhost:3000/d/token-metrics/web3-token-metrics-dashboard?orgId=1&refresh=10s&kiosk"
  width="100%"
  height="1000"
  frameborder="0">
</iframe>
```

---

## 📚 相关资源

- **Grafana 官方文档**: https://grafana.com/docs/
- **Prometheus 查询语言**: https://prometheus.io/docs/prometheus/latest/querying/basics/
- **代币统计指标**: `internal/engine/metrics_core.go`
- **处理器逻辑**: `internal/engine/processor_transfer.go`

---

**最后更新**: 2026-02-16
**维护者**: Claude Code
