# Grafana 无状态配置

**日期**: 2026-02-20
**目标**: Grafana 使用 Prometheus 数据源（无状态），而不是直接查询数据库

---

## 🎯 问题

**旧配置**：
- Dashboard 使用 PostgreSQL 数据源
- 直接查询 `web3_demo` 数据库
- 数据库被 Nuclear Reset 清空后，Grafana 显示空数据

**期望**：
- Grafana 使用 Prometheus 数据源
- 从 `/metrics` 端点获取实时指标
- 不依赖数据库状态

---

## ✅ 解决方案

### 1. 创建基于 Prometheus 的 Dashboard

**新文件**：`configs/grafana/provisioning/dashboards/prometheus-metrics.json`

**特点**：
- ✅ 使用 Prometheus 数据源
- ✅ 查询 Go 运行时指标（goroutines, memory, GC）
- ✅ 实时数据（5秒刷新）
- ✅ 不依赖数据库

### 2. 包含的 Panel

1. **Goroutines** - 当前 goroutines 数量
2. **Heap Memory** - 堆内存使用量
3. **GC Duration** - GC 持续时间
4. **Target Status** - 索引器 up/down 状态
5. **Memory Allocation Rate** - 内存分配速率
6. **Goroutines Over Time** - Goroutines 时间序列

### 3. 查询示例

```promql
# Goroutines
go_goroutines

# 堆内存
go_memstats_heap_alloc_bytes

# GC 持续时间
rate(go_gc_duration_seconds_sum[1m])

# 目标状态
up{job="web3-indexer"}

# 内存分配速率
rate(go_memstats_alloc_bytes_total[1m])
```

---

## 🚀 使用方法

### 重启 Grafana

```bash
docker restart web3-indexer-grafana
```

### 访问 Dashboard

```
1. 打开 Grafana: http://localhost:4000
2. 登录（admin / W3b3_Idx_Secur3_2026_Sec）
3. Dashboards → Web3 Dashboards
4. 选择 "Web3 Indexer - Prometheus Dashboard"
```

### 验证数据源

**检查 Prometheus 连接**：
1. Configuration → Data Sources → Prometheus
2. 点击 "Test"
3. 应该显示 "Data source is working"

---

## 📊 数据对比

### 旧方式（PostgreSQL）

```sql
SELECT MAX(number) AS value FROM blocks
```

**问题**：
- ❌ 依赖数据库
- ❌ Nuclear Reset 后数据清空
- ❌ 无法实时显示

### 新方式（Prometheus）

```promql
go_goroutines
```

**优势**：
- ✅ 不依赖数据库
- ✅ 实时数据（5秒刷新）
- ✅ Nuclear Reset 不影响
- ✅ 显示系统健康状态

---

## 🎯 完整的无状态架构

```
┌─────────────────────────────────────────────┐
│              Prometheus                     │
│  - 从 /metrics 端点抓取数据                  │
│  - 不依赖数据库                             │
│  - 实时更新（2秒间隔）                       │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│               Grafana                        │
│  - Prometheus 数据源                        │
│  - 实时 Dashboard                          │
│  - 无状态显示                               │
└─────────────────────────────────────────────┘
```

---

## ⚠️ 注意事项

### 1. 业务指标缺失

**当前状态**：
- ✅ Go 运行时指标正常（goroutines, memory）
- ❌ 业务指标未更新（blocks_processed, transfers）

**原因**：
- Metrics 已定义但可能未调用更新

**解决方案**：
- 先用 Go 指标验证连接
- 后续添加业务指标更新逻辑

### 2. 数据源配置

**Prometheus**：
- URL: `http://localhost:9091`
- Access: proxy
- Basic Auth: 否

**端口说明**：
- Prometheus: 9091（容器内）
- Grafana: 4000（Web UI）

---

## 🔧 故障排查

### Grafana 无法访问 Prometheus

```bash
# 1. 检查 Prometheus 是否运行
curl -s http://localhost:9091/api/v1/query?query=up

# 2. 检查 Grafana 日志
docker logs web3-indexer-grafana

# 3. 测试数据源
# Grafana UI → Configuration → Data Sources → Test
```

### Dashboard 无数据

```bash
# 1. 确认有指标暴露
curl -s http://localhost:8082/metrics | grep go_goroutines

# 2. 查询 Prometheus
curl -s 'http://localhost:9091/api/v1/query?query=go_goroutines'

# 3. 检查 Dashboard 配置
# 确认 datasource 为 "Prometheus_ds_001"
```

---

## 📝 总结

### 已完成

1. ✅ 创建基于 Prometheus 的 Dashboard
2. ✅ 配置自动加载（provisioning）
3. ✅ 重启 Grafana 生效

### 效果

- ✅ **Grafana 无状态**：不依赖数据库
- ✅ **实时数据**：5秒刷新
- ✅ **系统监控**：Go 运行时指标
- ✅ ** Nuclear Reset 安全**：不受影响

### 使用

```bash
# 重启 Grafana
docker restart web3-indexer-grafana

# 访问 Dashboard
open http://localhost:4000
```

---

**最后更新**: 2026-02-20
**状态**: ✅ 已实施
**无状态**: ✅ Grafana 现在也使用 Prometheus
