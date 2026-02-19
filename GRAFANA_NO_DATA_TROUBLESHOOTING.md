# Grafana 无数据问题排查

**日期**: 2026-02-20
**问题**: Grafana Dashboard 没有数据显示

---

## 🔍 问题诊断

### 1. Prometheus 状态

```bash
$ curl -s http://localhost:9091/api/v1/query?query=up
```

**结果**：
```json
{
  "data": {
    "result": [
      {"metric": {"instance": "demo", "job": "web3-indexer"}, "value": [..., "1"]},
      {"metric": {"instance": "sepolia", "job": "web3-indexer"}, "value": [..., "1"]}
    ]
  }
}
```

✅ **Prometheus 正常工作**
- ✅ demo (8082): up
- ✅ sepolia (8081): up

### 2. Indexer Metrics 端点

```bash
$ curl -s http://localhost:8082/metrics
```

**结果**：
```
# 只有 Go 运行时指标
go_gc_duration_seconds{...}
go_goroutines 17
go_memstats_alloc_bytes 6.94676e+06
...
```

❌ **没有业务指标**（`indexer_*`）

### 3. 根本原因

**Metrics 已定义但未更新**：
- ✅ Metrics 结构体定义完整（`internal/engine/metrics_core.go`）
- ✅ 使用 `promauto.NewCounter()` 自动注册
- ❌ 但指标可能从未被调用更新

---

## ✅ 解决方案

### 方案 1: 等待数据积累

**如果索引器刚启动**：
- 需要等待一些指标被更新
- 检查是否有块被处理

```bash
# 检查索引器状态
curl -s http://localhost:8082/api/status | jq '.latest_block, .total_blocks'
```

### 方案 2: 手动触发指标更新

**在代码中调用**：
```go
// 确保 Metrics 被初始化
metrics := GetMetrics()

// 更新指标
metrics.BlocksProcessed.Inc()
metrics.CurrentSyncHeight.Set(100)
```

### 方案 3: 检查指标是否被调用

**搜索代码中的指标更新**：
```bash
grep -r "BlocksProcessed.Inc" internal/
grep -r "CurrentSyncHeight.Set" internal/
grep -r "GetMetrics()" internal/
```

---

## 🔧 快速修复

### 1. 访问 Prometheus UI

```
http://localhost:9091
```

**查询**：
- `up` - 检查目标状态
- `go_goroutines` - 检查 Go 指标（应该有数据）
- `indexer_blocks_processed_total` - 检查业务指标（可能为空）

### 2. 访问 Grafana UI

```
http://localhost:4000
```

**检查**：
1. 登录（admin / W3b3_Idx_Secur3_2026_Sec）
2. Configuration → Data Sources → Prometheus
3. 点击 "Test" 检查连接
4. 检查 URL 是否为 `http://localhost:9091`

### 3. 导入 Dashboard

**Dashboard 位置**：
- `configs/grafana/provisioning/dashboards/`
- `grafana/*.json`

**导入步骤**：
1. Dashboards → New → Import
2. Upload JSON 文件
3. 选择 Prometheus 数据源

---

## 📊 验证步骤

### Step 1: 验证 Prometheus

```bash
# 查询 Go 指标（应该有数据）
curl -s 'http://localhost:9091/api/v1/query?query=go_goroutines'

# 查询业务指标（可能为空）
curl -s 'http://localhost:9091/api/v1/query?query=indexer_blocks_processed_total'
```

### Step 2: 验证 Grafana 数据源

1. 打开 Grafana: http://localhost:4000
2. Configuration → Data Sources
3. 点击 Prometheus
4. 点击 "Test"
5. 应该显示 "Data source is working"

### Step 3: 查看 Dashboard

**导入现有 Dashboard**：
- `Web3-Indexer-Dashboard-Enhanced.json`
- `Token-Analysis-Dashboard.json`

**或者创建新查询**：
- 查询: `go_goroutines`（应该有数据）
- 查询: `rate(go_gc_duration_seconds_sum[5m])`

---

## 🎯 推荐查询

### Go 运行时指标（应该有数据）

```
# Goroutines 数量
go_goroutines

# 内存使用
go_memstats_heap_alloc_bytes

# GC 持续时间
rate(go_gc_duration_seconds_sum[5m])
```

### 业务指标（需要等待数据）

```
# 已处理块数
indexer_blocks_processed_total

# 当前同步高度
indexer_current_sync_height

# 同步滞后
indexer_sync_lag
```

---

## ⚠️ 已知问题

### 1. 业务指标未暴露

**原因**：
- Metrics 已定义但可能未更新
- 需要检查代码中是否调用了 `.Inc()`, `.Set()`

**解决**：
- 检查 `GetMetrics()` 是否被调用
- 检查指标更新方法是否被调用

### 2. Prometheus 抓取间隔

**配置**：
```yaml
scrape_interval: 2s  # 每 2 秒抓取一次
```

**建议**：
- ✅ 当前配置合理
- ✅ 足够实时

---

## 📝 总结

### 当前状态

- ✅ Prometheus 正常运行（9091 端口）
- ✅ Grafana 数据源配置正确
- ✅ Go 运行时指标正常
- ❌ 业务指标未显示

### 下一步

1. **等待数据积累**：索引器处理一些块后
2. **检查指标更新**：确认代码中调用了指标更新方法
3. **使用 Go 指标**：先验证 Grafana 连接正常

### 验证命令

```bash
# 检查 Prometheus
curl -s http://localhost:9091/api/v1/query?query=go_goroutines

# 检查 Indexer
curl -s http://localhost:8082/api/status | jq '.latest_block'

# 访问 Grafana
open http://localhost:4000
```

---

**最后更新**: 2026-02-20
**状态**: ⚠️  需要进一步调查业务指标未更新的原因
