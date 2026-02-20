# Grafana iframe 更新到 Prometheus Dashboard

**日期**: 2026-02-20
**目标**: 更新 8082 的 iframe 显示新的 Prometheus Dashboard

---

## ✅ 已完成的修改

### 1. 创建新的 Prometheus Dashboard

**文件**: `configs/grafana/provisioning/dashboards/prometheus-metrics.json`

**特点**：
- ✅ 使用 Prometheus 数据源
- ✅ 显示 Go 运行时指标
- ✅ 不依赖数据库（无状态）
- ✅ 5秒自动刷新

**包含的 Panel**：
1. Goroutines
2. Heap Memory
3. GC Duration
4. Target Status
5. Memory Allocation Rate
6. Goroutines Over Time

### 2. 更新 HTML iframe 配置

**文件**: `internal/web/dashboard.html`

**修改内容**：
```javascript
// 旧代码（使用 PostgreSQL Dashboard）
const dashboardPath = `/d/web3-indexer-multi/web3-indexer-multi?orgId=1&refresh=5s&kiosk&var-Environment=${currentEnv}&var-PostgresDS=${currentDS}`;

// 新代码（使用 Prometheus Dashboard）
const dashboardPath = `/d/web3-indexer-prometheus/web3-indexer-prometheus?orgId=1&refresh=5s&kiosk`;
```

**效果**：
- ✅ 移除了数据库依赖
- ✅ 简化了 dashboard 路径
- ✅ 添加了控制台日志输出

### 3. 重新构建和部署

```bash
# 重新构建镜像
docker build -t web3-indexer-go:stable .

# 重启容器
docker stop web3-indexer-app
docker rm web3-indexer-app
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d indexer
```

---

## 📊 访问方式

### 8082 端口（Docker）

```
http://localhost:8082
```

**Grafana iframe 区域**：
- 显示新的 Prometheus Dashboard
- 实时数据（5秒刷新）
- 不依赖数据库状态

### 独立访问 Grafana

```
http://localhost:4000
```

**登录信息**：
- 用户名: `admin`
- 密码: `W3b3_Idx_Secur3_2026_Sec`

**Dashboard 位置**：
- Dashboards → Web3 Dashboards
- 选择 "Web3 Indexer - Prometheus Dashboard"

---

## 🎯 效果对比

### 旧配置（PostgreSQL Dashboard）

```javascript
/d/web3-indexer-multi/web3-indexer-multi?var-Environment=demo&var-PostgresDS=web3_demo
```

**问题**：
- ❌ 依赖数据库查询
- ❌ Nuclear Reset 后无数据
- ❌ 使用模板变量（复杂）

### 新配置（Prometheus Dashboard）

```javascript
/d/web3-indexer-prometheus/web3-indexer-prometheus?orgId=1&refresh=5s&kiosk
```

**优势**：
- ✅ 不依赖数据库
- ✅ Nuclear Reset 安全
- ✅ 实时指标
- ✅ 路径简化

---

## 🔍 验证方法

### 1. 访问 8082 页面

```bash
open http://localhost:8082
```

### 2. 检查 iframe 内容

**浏览器开发者工具**：
1. 右键点击 Grafana 区域 → Inspect
2. 查找 `<iframe>` 标签
3. 检查 `src` 属性

**预期 src**：
```
http://localhost:4000/d/web3-indexer-prometheus/web3-indexer-prometheus?orgId=1&refresh=5s&kiosk
```

### 3. 查看浏览器控制台

**预期日志**：
```
🧬 Prometheus Dashboard Loaded (Stateless): demo
```

### 4. 验证数据显示

**应该看到**：
- ✅ Goroutines: ~17
- ✅ Heap Memory: 几 MB
- ✅ Target Status: UP（绿色）

---

## ⚠️ 注意事项

### Dashboard UID

**新 Dashboard UID**: `web3-indexer-prometheus`

**文件位置**：
```
configs/grafana/provisioning/dashboards/prometheus-metrics.json
```

**自动加载**：
- Grafana provisioning 会自动加载这个 dashboard
- 无需手动导入

### 如果看不到新 Dashboard

**重启 Grafana**：
```bash
docker restart web3-indexer-grafana
```

**或者手动导入**：
1. Grafana UI → Dashboards → New → Import
2. 上传 `configs/grafana/provisioning/dashboards/prometheus-metrics.json`
3. 选择 Prometheus 数据源

---

## 📝 总结

### 已完成

1. ✅ 创建 Prometheus Dashboard（6 个 Panel）
2. ✅ 更新 HTML iframe 配置
3. ✅ 重新构建 Docker 镜像
4. ✅ 重启 8082 容器

### 效果

- ✅ **iframe 显示新 Dashboard**
- ✅ **无状态监控**（不依赖数据库）
- ✅ **实时数据**（5秒刷新）
- ✅ **Nuclear Reset 安全**

### 访问

```bash
# 8082 主页面
open http://localhost:8082

# 独立 Grafana
open http://localhost:4000
```

---

**最后更新**: 2026-02-20
**状态**: ✅ 已部署
**Dashboard**: Prometheus（无状态）
