# Grafana Dashboard - 增强版导入指南

## 🎯 v2.0 增强版改进（2026-02-15）

基于生产环境反馈，v2.0 版本针对以下问题进行了**精确修复**：

### 1️⃣ 修复 "Hash == Parent Hash" 展示问题

**问题原因**:
- 原版 Dashboard 没有包含 "Latest Blocks" Table 面板
- 用户看到的哈希重复来自**其他 Table 面板的字段映射错误**

**v2.0 解决方案**:
- ✅ 新增 **PostgreSQL 数据源的 "Latest Blocks (Top 10)" Table 面板**
- ✅ SQL 查询**明确指定字段别名**，避免 `SELECT *`:
  ```sql
  SELECT
    number AS "Block #",
    substring(hash, 1, 10) AS "Hash",
    substring(parent_hash, 1, 10) AS "Parent Hash",
    gas_used AS "Gas Used",
    tx_count AS "Tx Count",
    to_char(processed_at, 'HH24:MI:SS') AS "Processed At"
  FROM blocks
  ORDER BY number DESC
  LIMIT 10;
  ```
- ✅ 使用 `substring(hash, 1, 10)` 显示前 10 个字符，**视觉上区分 Hash 和 Parent Hash**

**配置细节**:
- **Transformation**: `organize` - 排除 `Time` 列，避免混淆
- **Sort**: 按 `Block #` 降序（最新块在前）
- **Align**: `auto` - 自动对齐列

---

### 2️⃣ 修复 Sync Lag 阈值（避免误导性的"百万级"）

**问题原因**:
- 原版阈值设置为 80（太低），导致正常显示时触发红色警告
- PromQL 使用 `COUNT` 而非 `MAX`，但这个问题已在代码层修复

**v2.0 解决方案**:
- ✅ 调整阈值为合理的生产级别:
  ```json
  "thresholds": {
    "steps": [
      {"color": "green", "value": null},     // 0-999: 绿色
      {"color": "yellow", "value": 1000},    // 1000-4999: 黄色
      {"color": "red", "value": 5000}        // >= 5000: 红色
    ]
  }
  ```
- ✅ PromQL 确保使用正确的 Gauge 指标: `indexer_sync_lag_blocks`

**面试话术**:
> "Sync Lag 的阈值设置需要根据业务场景调整。
> 对于演示环境，我设置为 1000/5000，避免误报。
> 生产环境中，这个值应该根据团队的风险容忍度来设定。"

---

### 3️⃣ 修复 TPS 精度（解决 9.222508... 显示问题）

**问题原因**:
- 缺少 `decimals` 配置，Grafana 默认显示高精度浮点数

**v2.0 解决方案**:
- ✅ 添加 `decimals: 2` 配置:
  ```json
  "decimals": 2,
  "max": 20          // 设置 Y 轴上限为 20，避免波动
  ```
- ✅ Legend 计算改为 `["last"]`，显示当前值而非平均值

**效果**:
- **之前**: `9.222508...`
- **现在**: `9.22`

---

### 4️⃣ 修复 E2E Latency 单位显示

**问题原因**:
- 原版使用 `unit: "s"`（秒），但 Grafana 的秒单位显示为 "1d 3h 20m" 格式
- 面试演示时，这个格式不够直观

**v2.0 解决方案**:
- ✅ 改用 `unit: "dtdurations"`（Duration - Time Durations）
- ✅ Grafana 会自动选择合适的单位:
  - < 60s: 显示秒
  - 1m-60m: 显示分钟
  - 1h-24h: 显示小时
  - > 24h: 显示天

**效果**:
- **之前**: `1608` (秒，不直观)
- **现在**: `26m 48s` (26 分 48 秒，直观)

---

### 5️⃣ 优化 RPC Health 显示

**问题原因**:
- 原版只显示数字（0, 1, 2），不够直观

**v2.0 解决方案**:
- ✅ 添加 Value Mapping:
  ```json
  "mappings": [
    {
      "options": {
        "0": {"text": "0 Nodes", "color": "red"},
        "1": {"text": "1 Node", "color": "yellow"},
        "2": {"text": "2 Nodes", "color": "green"}
      },
      "type": "value"
    }
  ]
  ```

**效果**:
- **之前**: `2` (数字)
- **现在**: `2 Nodes` (带文本，绿色背景)

---

### 6️⃣ 优化 DB Latency 单位

**问题原因**:
- Prometheus histogram 指标默认是秒，但 DB 延迟通常用毫秒表示

**v2.0 解决方案**:
- ✅ PromQL 乘以 1000 转换为毫秒:
  ```promql
  histogram_quantile(0.95, indexer_db_query_duration_seconds_bucket) * 1000
  ```
- ✅ 单位设置为 `unit: "ms"`

**效果**:
- **之前**: `0.002` (秒，不直观)
- **现在**: `2.15 ms` (毫秒，直观)

---

## 📊 v2.0 面板清单（11 个面板）

### 第一行：核心状态（Top Row）

1. **Sync Lag (Blocks Behind)** - 改进阈值
2. **Real-time TPS** - 添加精度控制
3. **E2E Latency** - 改进单位显示
4. **RPC Health** - 添加文本映射

### 第二行：Latest Blocks Table（NEW!）

5. **Latest Blocks (Top 10)** - 新增 PostgreSQL Table 面板
   - 显示区块号、Hash、Parent Hash、Gas Used、Tx Count、处理时间
   - **明确区分 Hash 和 Parent Hash**
   - 按区块号降序排列

### 第三行：性能监控（Performance）

6. **RPC Consumption (QPS)** - 颜色编码
7. **Block Height Tracking** - 双线图
8. **Database Performance** - 毫秒单位

### 第四行：吞吐量和缓冲区（Throughput）

9. **Processing Throughput** - 添加精度
10. **Sequencer Buffer**
11. **Self-Healing Count**

---

## 🚀 导入步骤（与 v1 相同）

### 方法 1：通过 Grafana UI（推荐）

1. 打开 Grafana：`http://localhost:3000`

2. 登录（默认：admin/admin）

3. 点击左侧菜单 **"+"** → **"Import"**

4. 选择 **"Upload JSON file"**

5. 上传文件：`grafana/Web3-Indexer-Dashboard-Enhanced.json`

6. **重要**：配置 PostgreSQL 数据源（名为 "Web3-Sepolia-DB"）
   - Host: `web3-indexer-sepolia-db:5432`
   - Database: `web3_sepolia`
   - User: `postgres`
   - Password: 从 `.env.testnet.local` 读取

7. 选择 Prometheus 数据源

8. 点击 **"Import"**

### 方法 2：通过 Grafana API

```bash
curl -X POST \
  http://localhost:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_GRAFANA_API_KEY" \
  -d @grafana/Web3-Indexer-Dashboard-Enhanced.json
```

---

## 🔧 配置 PostgreSQL 数据源（重要！）

v2.0 版本需要配置 PostgreSQL 数据源用于 "Latest Blocks" Table 面板。

### 在 Grafana UI 中添加数据源

1. 打开 Grafana
2. 点击左侧菜单 **"Configuration"** (齿轮图标) → **"Data sources"**
3. 点击 **"Add data source"**
4. 选择 **"PostgreSQL"**
5. 配置:
   ```
   Name: Web3-Sepolia-DB
   Host: web3-indexer-sepolia-db:5432
   Database: web3_sepolia
   User: postgres
   Password: your_password
   SSL mode: disable
   ```
6. 点击 **"Save & Test"**

---

## 💡 面试话术（Dashboard v2.0 改进）

当面试官询问 Dashboard 的改进时：

> "v1.0 版本上线后，我收到了用户的两个反馈：
>
> 1. **'Hash == Parent Hash' 显示问题**：用户在 Table 面板中看到 Hash 列和 Parent Hash 列显示相同的内容。
>    **我的排查**：首先用 SQL 验证数据库物理层，确认不存在哈希自指问题（返回 0 行）。
>    **根因分析**：原版 Dashboard 没有包含 Latest Blocks Table，用户看到的是其他面板的字段映射错误。
>    **解决方案**：新增 PostgreSQL Table 面板，使用明确的字段别名和 `substring()` 函数，视觉上区分两个列。
>
> 2. **TPS 显示长浮点数**：Real-time TPS 显示 9.222508...，不美观。
>    **解决方案**：添加 `decimals: 2` 配置，限制小数点后 2 位。
>
> 3. **Sync Lag 阈值太低**：原版阈值 80 导致正常状态触发红色警告。
>    **解决方案**：调整为 1000/5000，符合生产环境的实际需求。
>
> 4. **E2E Latency 单位不直观**：显示 1608 秒，面试演示时观众需要心算。
>    **解决方案**：改用 Grafana 的 `dtdurations` 单位，自动显示为 '26m 48s'。
>
> 整个修复过程遵循了'**物理层验证 → 配置层修复**'的方法论，避免盲目修改数据。"

---

## 🎯 验证测试

导入 Dashboard 后，验证以下改进：

### 测试 1：Latest Blocks Table
- **预期**：Hash 和 Parent Hash 列显示**不同的值**
- **验证**：
  ```sql
  -- 在 Grafana Query Editor 中运行
  SELECT number, hash, parent_hash
  FROM blocks
  ORDER BY number DESC
  LIMIT 5;
  ```

### 测试 2：TPS 精度
- **预期**：Real-time TPS 面板显示 `9.22` 而非 `9.222508...`
- **验证**：查看面板图例

### 测试 3：Sync Lag 阈值
- **预期**：Sync Lag < 1000 时显示绿色，> 1000 时显示黄色
- **验证**：查看面板颜色

### 测试 4：E2E Latency 单位
- **预期**：显示 "26m 48s" 而非 "1608s"
- **验证**：查看面板

---

## 🔄 从 v1 迁移到 v2

如果你已经导入了 v1 Dashboard：

### 选项 1：保留两个版本
- v1 (uid: `web3-indexer-prod`) - 用于对比
- v2 (uid: `web3-indexer-prod-v2`) - 生产使用

### 选项 2：删除 v1，使用 v2
```bash
# 删除旧 Dashboard
curl -X DELETE \
  http://localhost:3000/api/dashboards/uid/web3-indexer-prod \
  -H "Authorization: Bearer YOUR_API_KEY"

# 导入新 Dashboard
# (按上面的导入步骤)
```

---

## 🐛 故障排查

### 问题 1：Latest Blocks Table 显示 "No data"

**原因**：PostgreSQL 数据源未配置或连接失败

**解决**：
1. 检查数据源配置（见上面的 "配置 PostgreSQL 数据源"）
2. 测试连接：点击 "Save & Test"
3. 验证 SQL 查询在 Grafana Query Editor 中运行

### 问题 2：Hash 和 Parent Hash 仍然显示相同

**原因**：Transformation 配置错误

**解决**：
1. 编辑面板
2. 打开 **Transformations** 标签页
3. 确保 `organize` transformation 存在
4. 确保 `excludeByName` 包含 `"Time": true`

### 问题 3：E2E Latency 仍显示秒数

**原因**：浏览器缓存了旧 Dashboard

**解决**：
1. 硬刷新浏览器：`Ctrl + Shift + R` (Windows/Linux) 或 `Cmd + Shift + R` (Mac)
2. 或重新导入 Dashboard

---

## 📈 性能优化建议

### 1. 减少 PostgreSQL 查询频率

默认情况下，Table 面板每 5 秒刷新一次。如果 PostgreSQL 压力大，可以：

**方法 A**：增加 Dashboard 刷新间隔
```json
"refresh": "30s"  // 或 "1m"
```

**方法 B**：使用 Time Series 面板替代 Table
- 不推荐，Table 面板用于展示详细数据

### 2. 限制 Table 面板行数

当前 `LIMIT 10`。如果需要更多行：
```sql
LIMIT 20  -- 或 50
```

**注意**：行数过多会影响 Dashboard 加载速度。

---

## 🎨 自定义建议

### 调整 Latest Blocks 显示列

如果你想显示更多列，修改 SQL 查询：

```sql
SELECT
  number AS "Block #",
  substring(hash, 1, 16) AS "Hash",           -- 16 个字符
  substring(parent_hash, 1, 16) AS "Parent",  -- 16 个字符
  gas_used AS "Gas Used",
  gas_limit AS "Gas Limit",
  tx_count AS "Tx Count",
  to_char(processed_at, 'HH24:MI:SS') AS "Processed At"
FROM blocks
ORDER BY number DESC
LIMIT 10;
```

### 添加颜色编码到 Table

在 Table 面板的 Field Config 中添加 Thresholds：

```json
{
  "fieldConfig": {
    "defaults": {
      "thresholds": {
        "mode": "absolute",
        "steps": [
          {"color": "green", "value": null},
          {"color": "yellow", "value": 1000000},   // Gas Used > 1M
          {"color": "red", "value": 5000000}       // Gas Used > 5M
        ]
      }
    },
    "overrides": [
      {
        "matcher": {"id": "byName", "options": "Gas Used"},
        "properties": [
          {"id": "thresholds", "value": {...}}
        ]
      }
    ]
  }
}
```

---

**文档版本**：v2.0
**Dashboard 版本**：2
**最后更新**：2026-02-15
**维护者**：追求 6 个 9 持久性的资深后端

---

## 🎉 总结

v2.0 版本通过以下改进解决了生产环境的反馈：

✅ **新增 PostgreSQL Table 面板**，明确区分 Hash 和 Parent Hash
✅ **TPS 精度控制**，显示 9.22 而非 9.222508...
✅ **Sync Lag 阈值优化**，调整为 1000/5000
✅ **E2E Latency 单位改进**，自动显示 "26m 48s"
✅ **RPC Health 文本映射**，显示 "2 Nodes" 而非 "2"
✅ **DB Latency 毫秒单位**，显示 "2.15 ms" 而非 "0.002"

所有改进都基于**物理层验证 → 配置层修复**的方法论，确保数据准确性和用户体验。
