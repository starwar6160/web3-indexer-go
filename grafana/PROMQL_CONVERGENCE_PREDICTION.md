# Grafana PromQL - 收敛时间预测查询

## 目的

预测 Indexer 需要多少分钟才能追上链头（Sync Lag = 0），基于当前的处理速度。

---

## 1. 估算剩余时间（秒）

### 方法 A: 基于当前块处理速度

```promql
# 计算公式：剩余块数 / (每秒处理块数)
# (Sync Lag × 12) / (当前 BPS × 60)

(indexer_sync_lag_blocks * 12) / (
  (rate(indexer_blocks_processed_total[5m]) * 60) > 0
)
```

**说明**:
- `indexer_sync_lag_blocks`: 当前落后块数
- `rate(indexer_blocks_processed_total[5m])`: 过去 5 分钟的块处理速率
- `× 60`: 将每秒速率转换为每分钟速率
- `× 12`: Sepolia 出块时间（秒）

**预期结果**:
- 追赶模式: `450 秒`（约 7.5 分钟）
- 实时模式: `0 秒`（已经追上）

---

### 方法 B: 基于历史平均速度

```promql
# 使用过去 1 小时的平均处理速度

(indexer_sync_lag_blocks * 12) / (
  avg_over_time(rate(indexer_blocks_processed_total[5m])[1h:5m]) * 60
)
```

**说明**:
- `avg_over_time(...)[1h:5m]`: 过去 1 小时的滑动平均值
- 更平滑，适合长期预测

---

## 2. 估算剩余块数（如果不想用时间）

```promql
# 直接显示剩余块数
indexer_sync_lag_blocks
```

**配置建议**:
- 单位: `short` (无单位)
- 阈值:
  - < 10: 绿色（实时）
  - 10-100: 黄色（追赶中）
  - > 100: 红色（严重滞后）

---

## 3. 估算完成百分比

```promql
# 已完成百分比
100 * (
  1 - (indexer_sync_lag_blocks / indexer_current_chain_height)
)
```

**说明**:
- 如果 Sync Lag = 100，Chain Height = 10,000,000
- 完成百分比 = 100 × (1 - 100/10,000,000) = 99.999%

**配置建议**:
- 单位: `percent (0-100)`
- 阈值:
  - 99.9%: 绿色（接近完成）
  - 99%: 黄色
  - < 99%: 红色

---

## 4. 实时 vs 追赶模式检测

```promql
# 判断是否处于实时模式
indexer_sync_lag_blocks < 10
```

**用途**:
- 返回 `1` = 实时模式（Sync Lag < 10）
- 返回 `0` = 追赶模式（Sync Lag >= 10）

**可用于**:
- 状态面板
- 告警规则
- 变量模板

---

## 5. 完整的 Dashboard 面板配置

### Panel 1: 预计剩余时间

**Title**: `Estimated Time to Catch-up`

**Query**:
```promql
(indexer_sync_lag_blocks * 12) / (
  (rate(indexer_blocks_processed_total[5m]) * 60) > 0
)
```

**Config**:
- Unit: `durations` (自动选择秒/分钟/小时)
- Decimals: `0` (整数)
- Thresholds:
  - < 60s: 绿色
  - 60s-600s: 黄色
  - > 600s: 红色

---

### Panel 2: 处理速度趋势

**Title**: `Block Processing Speed`

**Query**:
```promql
rate(indexer_blocks_processed_total[5m]) * 60
```

**Config**:
- Unit: `blocks/min` (每分钟块数)
- Legend: `Processing Speed`
- Type: Time series

---

### Panel 3: 追赶进度

**Title**: `Catch-up Progress`

**Query**:
```promql
100 * (1 - (indexer_sync_lag_blocks / indexer_current_chain_height))
```

**Config**:
- Unit: `percent (0-100)`
- Min: `0`
- Max: `100`
- Decimals: `3` (显示小数)

---

## 6. 告警规则示例

```yaml
# alerting_rules.yml
groups:
  - name: indexer_convergence
    rules:
      # 告警：追赶时间超过 10 分钟
      - alert: IndexerNotConverging
        expr: |
          (indexer_sync_lag_blocks * 12) / (
            (rate(indexer_blocks_processed_total[5m]) * 60) > 0
          ) > 600
        for: 5m
        annotations:
          summary: "Indexer not converging"
          description: "Estimated {{ $value }} seconds to catch up"

      # 告警：进入实时模式
      - alert: IndexerInRealtimeMode
        expr: indexer_sync_lag_blocks < 10
        for: 1m
        annotations:
          summary: "Indexer in realtime mode"
          description: "Sync Lag is {{ $value }} blocks"
```

---

## 7. 故障排查

### 问题 1: 显示 Infinity 或 NaN

**原因**:
- `rate()` 返回 0（没有处理块）
- 除以 0 导致无穷大

**解决**:
```promql
# 添加 > 0 过滤器
(rate(indexer_blocks_processed_total[5m]) * 60) > 0
```

---

### 问题 2: 预测时间波动大

**原因**:
- 使用了 5 分钟短期窗口，不够平滑

**解决**:
```promql
# 使用更长的时间窗口
avg_over_time(rate(indexer_blocks_processed_total[5m])[1h:5m])
```

---

## 8. 实际使用案例

### 案例 1: 快速追赶（QPS=10）

```
Sync Lag: 1000 块
处理速度: 10 块/秒 (600 块/分钟)
预计时间: (1000 × 12) / 600 = 20 秒
```

### 案例 2: 慢速追赶（QPS=1，演示模式）

```
Sync Lag: 1000 块
处理速度: 0.25 块/秒 (15 块/分钟)
预计时间: (1000 × 12) / 15 = 800 秒 ≈ 13 分钟
```

### 案例 3: 实时模式（Sync Lag < 10）

```
Sync Lag: 5 块
处理速度: 忽略（已经在实时模式）
预计时间: 0 秒（已追上）
```

---

**文档版本**: v1.0
**最后更新**: 2026-02-15
**适用场景**: Web3 Indexer 生产监控
