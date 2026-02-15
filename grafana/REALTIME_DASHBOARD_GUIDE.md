# Grafana Dashboard - 实时监控版（修复单位显示）

## 🎯 v3.0 实时监控版改进（2026-02-15）

基于用户反馈，v3.0 版本专注于**实时监控和单位修复**：

### 1️⃣ 修复 E2E Latency 单位显示

**问题**: 用户反馈显示 `14967ms`（毫秒），不够直观

**解决方案**:
- ✅ 单位改为 `s`（秒）
- ✅ 小数精度设为 `2`（2 位小数）
- ✅ 添加阈值颜色编码

**效果**:
```
修复前: 14967ms (不直观，需要心算)
修复后: 14.97s (直观，专业)
```

**配置细节**:
```json
{
  "unit": "s",
  "decimals": 2,
  "thresholds": {
    "steps": [
      {"color": "green", "value": null},    // 0-30s: 绿色
      {"color": "yellow", "value": 30},     // 30-60s: 黄色
      {"color": "orange", "value": 60},     // 60-120s: 橙色
      {"color": "red", "value": 120}        // > 120s: 红色
    ]
  }
}
```

---

### 2️⃣ 优化 Sync Lag 显示

**改进**:
- ✅ 单位: `short`（纯数字）
- ✅ 精度: `0`（整数，无小数）
- ✅ 最大值: `100`（突出显示实时状态）

**效果**:
```
实时模式: "1" (清晰)
追赶模式: "322" (可读)
```

---

### 3️⃣ 优化 TPS 精度

**改进**:
- ✅ 精度: `2`（2 位小数）
- ✅ 最大值: `20`（避免波动过大）

**效果**:
```
修复前: 10.294117647058824
修复后: 10.56
```

---

## 📊 v3.0 面板清单（4 个核心面板）

### 第一行：核心状态（Top Row）

1. **Sync Lag (Blocks)** - 优化精度和阈值
2. **Real-time TPS** - 添加 2 位小数精度
3. **E2E Latency (seconds)** - ✨ 修复单位为秒，2 位小数
4. **RPC Health** - 保持不变

---

## 🚀 导入步骤

### 方法 1：通过 Grafana UI

1. 打开 Grafana：`http://localhost:3000`

2. 登录（默认：admin/admin）

3. 点击左侧菜单 **"+"** → **"Import"**

4. 选择 **"Upload JSON file"**

5. 上传文件：`grafana/Web3-Indexer-Dashboard-Realtime.json`

6. 选择 Prometheus 数据源

7. 点击 **"Import"**

---

## 🎨 自定义建议

### 调整 E2E Latency 阈值

当前配置（适合实时模式）:

```json
"thresholds": {
  "steps": [
    {"color": "green", "value": null},    // < 30s: 绿色
    {"color": "yellow", "value": 30},     // 30-60s: 黄色
    {"color": "orange", "value": 60},     // 60-120s: 橙色
    {"color": "red", "value": 120}        // > 120s: 红色
  ]
}
```

**追赶模式建议**（如果系统经常追赶）:

```json
"thresholds": {
  "steps": [
    {"color": "green", "value": null},    // < 5m: 绿色
    {"color": "yellow", "value": 300},    // 5-10m: 黄色
    {"color": "orange", "value": 600},    // 10-20m: 橙色
    {"color": "red", "value": 1200}       // > 20m: 红色
  ]
}
```

**注意**: 需要同时调整 `unit` 为 `duration`（自动格式化为分钟/小时）

---

### 调整小数精度

**E2E Latency**（当前 2 位小数）:
```json
"decimals": 2  // 14.97s
```

**如果需要更精确**:
```json
"decimals": 3  // 14.967s
```

**如果只需要整数秒**:
```json
"decimals": 0  // 15s
```

---

## 💡 面试话术（Dashboard v3.0 改进）

当面试官询问这次修复时：

> "用户反馈 E2E Latency 显示为 `14967ms`，不够直观。
>
> **我的分析**：
> 1. 问题不在于数据（计算正确：14967 毫秒 = 14.967 秒）
> 2. 问题在于单位显示（毫秒不直观，需要心算）
> 3. 参考行业最佳实践，监控系统应该使用人类可读的单位
>
> **解决方案**：
> 1. 修改 Grafana 配置，单位从 `ms` 改为 `s`
> 2. 设置 `decimals: 2`，显示 2 位小数（14.97s）
> 3. 添加阈值颜色编码（绿色 < 30s，黄色 30-60s，红色 > 120s）
>
> **工程价值**：
> - 提升用户体验（14.97s > 14967ms）
> - 符合认知习惯（秒级延迟适合实时系统）
> - 颜色编码快速识别问题（红色 = 需要关注）
>
> **关键洞察**：
> '数据正确 ≠ 显示友好'，优秀的监控系统应该让数据一目了然。"

---

## 🔍 故障排查

### 问题 1：E2E Latency 仍显示毫秒

**原因**: 浏览器缓存了旧 Dashboard

**解决**:
1. 硬刷新浏览器：`Ctrl + Shift + R` (Windows/Linux) 或 `Cmd + Shift + R` (Mac)
2. 或重新导入 Dashboard

---

### 问题 2：小数点后仍显示多位数字

**原因**: API 返回的数据精度太高

**解决**: 已经在代码层修复（`math.Round(tps*100)/100`），Grafana 只是显示层

---

### 问题 3：阈值颜色不生效

**原因**: PromQL 返回的值可能为 0 或 null

**解决**: 检查 Prometheus metrics 是否正常暴露
```bash
curl http://localhost:8081/metrics | grep indexer_sync_lag_blocks
```

---

## 📈 与之前版本的区别

### v1.0 (基础版)
- E2E Latency: `1618044ms` (原始毫秒数)
- TPS: `10.294117647058824` (16 位小数)
- Sync Lag: 阈值 80（太低）

### v2.0 (增强版)
- E2E Latency: `"Catching up... 322 blocks remaining"` (智能显示)
- TPS: `10.56` (2 位小数)
- Sync Lag: 阈值 1000/5000（合理）

### v3.0 (实时监控版) ✨ 当前版本
- E2E Latency: `14.97s` (秒 + 2 位小数 + 颜色编码)
- TPS: `10.56` (保持 2 位小数)
- Sync Lag: `1` (实时模式，整数显示)
- **特点**: 专注于实时监控，单位人性化

---

## 🎯 使用场景

### 场景 1: 实时监控（推荐）

适用情况：系统已经进入实时模式（Sync Lag < 10）

```bash
# 导入 v3.0 Dashboard
# 观察指标：
# - Sync Lag: 1-5 块
# - E2E Latency: 12-60 秒（绿色）
# - TPS: 10-15（Sepolia 实际速率）
```

### 场景 2: 追赶监控

适用情况：系统正在追赶历史数据（Sync Lag > 100）

```bash
# 导入 v2.0 Dashboard（带智能显示）
# 观察指标：
# - E2E Latency: "Catching up... 322 blocks remaining"
# - 颜色编码帮助识别进度
```

### 场景 3: 完整监控

适用情况：需要同时监控实时状态和历史数据

```bash
# 导入 v2.0（功能最全）
# 包含 11 个面板，涵盖所有指标
```

---

## 🚀 下一步优化

1. **添加收敛时间预测面板**
   - 使用 PromQL 计算预计剩余时间
   - 参考 `grafana/PROMQL_CONVERGENCE_PREDICTION.md`

2. **添加区块处理耗时面板**
   - 使用 SQL 查询数据库写入延迟
   - 参考 `scripts/sql-block-processing-latency.sql`

3. **添加告警规则**
   - Sync Lag > 100 块
   - E2E Latency > 5 分钟
   - RPC Health < 2 节点

---

**文档版本**: v3.0
**Dashboard 版本**: 3
**最后更新**: 2026-02-15
**维护者**: 追求 6 个 9 持久性的资深后端

---

## 🎉 总结

v3.0 版本通过以下改进解决了用户反馈：

✅ **E2E Latency 单位修复**: 14967ms → 14.97s（秒 + 2 位小数）
✅ **阈值颜色编码**: 绿色 < 30s，黄色 30-60s，红色 > 120s
✅ **TPS 精度保持**: 10.56（2 位小数）
✅ **Sync Lag 优化**: 实时模式显示整数

所有改进都基于**用户体验优先**的原则，确保监控系统既准确又友好。
