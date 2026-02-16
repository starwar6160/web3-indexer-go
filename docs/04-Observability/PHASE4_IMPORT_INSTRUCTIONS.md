# ✅ Phase 4 完成 - Grafana Dashboard 导入指南

## 🎯 当前状态

**Phase 4**: Grafana Dashboard 配置 - ✅ **代码已完成**
**总体进度**: 95% (4/5 phases complete)

---

## 📋 下一步操作（3 步）

### Step 1: 打开浏览器访问 Grafana

```
http://localhost:4000
```

**登录凭据**:
- 用户名: `admin`
- 密码: `W3b3_Idx_Secur3_2026_Sec`

### Step 2: 确认 Prometheus 数据源

1. 左侧菜单 → **Configuration** (⚙️) → **Data sources**
2. 检查是否有 **Prometheus** 数据源
3. 点击 **Test**，确认显示 "Data source is working"

如果没有，点击 **Add data source** → 选择 **Prometheus**：
- Name: `Prometheus`
- URL: `http://localhost:9091`

### Step 3: 导入 Dashboard

1. 左侧菜单 → **Dashboards** → **Import**
2. 点击 **"Upload JSON file"**
3. 选择文件:
   ```
   /home/ubuntu/zwCode/web3-indexer-go/grafana/Token-Metrics-Dashboard.json
   ```
4. 数据源选择: **Prometheus**
5. 点击 **Import**

---

## ✅ 验证清单

导入成功后，你应该看到：

### 7 个面板

1. ✅ **USDC 过去 1 小时流水** - 可能显示 0（刚启动）
2. ✅ **过去 1 小时总转账数** - 可能显示 0（刚启动）
3. ✅ **24 小时代币转账趋势** - 折线图（暂时为空）
4. ✅ **四大热门代币转账次数占比** - 饼图（暂时为空）
5. ✅ **实时转账速率（TPS）** - 折线图（暂时为空）
6. ✅ **🛡️ RPC QUOTA GUARD (DAILY)** - 应显示 `0.04%`（绿色，安全）
7. ✅ **24 小时代币活动详细统计** - 表格（暂时为空）

### 关键验证点

- [ ] Dashboard 标题: "Web3 Token Metrics Dashboard"
- [ ] 面板 6 (RPC 额度) 显示绿色小数值（如 0.04%）
- [ ] 无红色错误信息（"No Data" 是正常的，刚启动还没有数据）
- [ ] 刷新频率设置为 10 秒（默认）

---

## ⏳ 等待数据（10-15 分钟）

系统刚启动，需要等待一些时间处理新的区块：

### 当前系统状态
```bash
✅ Indexer: 运行中（14 分钟）
✅ Prometheus: 正常抓取指标
✅ RPC 额度: 0.04% (安全)
⏳ 代币统计: 等待 Transfer 事件
```

### 预期行为

**等待 10-15 分钟后**:
- USDC/DAI/WETH/UNI 转账数据开始出现
- 饼图显示代币占比
- TPS 折线图开始更新

**如果一直没有数据**:
```bash
# 检查 Indexer 日志
docker logs web3-debug-app | grep "Processing block"

# 查看 Prometheus 指标
curl http://localhost:8083/metrics | grep indexer_token_transfer

# 检查数据库中的转账记录
docker exec -it web3-debug-db psql -U postgres -d web3_indexer \
  -c "SELECT COUNT(*) FROM transfers;"
```

---

## 🔧 如果遇到问题

### 问题 1: Dashboard 导入失败

**原因**: 数据源未配置或 URL 错误

**解决**:
1. 确认 Prometheus 数据源已配置
2. URL 应该是: `http://localhost:9091`
3. 点击 "Test" 确认连接成功

### 问题 2: 面板显示 "No Data"

**原因**: Prometheus 查询失败

**解决**:
```bash
# 检查 Prometheus 是否有指标
curl -s 'http://localhost:9091/api/v1/query?query=rpc_quota_usage_percent' | jq '.data.result[0]'

# 应该返回:
# {
#   "metric": {},
#   "value": [1708021234, "0.04"]
# }
```

### 问题 3: Grafana 无法访问

**原因**: 容器未运行或端口被占用

**解决**:
```bash
# 检查容器状态
docker ps | grep grafana

# 如果没有运行，启动它
docker start web3-indexer-grafana

# 检查端口
netstat -tuln | grep :4000
```

---

## 📚 相关文档

1. **详细导入指南**: `IMPORT_DASHBOARD_GUIDE.md`
2. **Phase 4 完成总结**: `PHASE4_COMPLETION_SUMMARY.md`
3. **Dashboard JSON**: `grafana/Token-Metrics-Dashboard.json`

---

## 🎉 完成后

### Phase 4 验收标准

- [x] Dashboard JSON 配置完成
- [x] 导入指南文档完成
- [ ] 用户成功导入 Dashboard
- [ ] 验证 RPC 额度仪表盘正常
- [ ] 等待 15 分钟验证代币统计面板

### 下一步: Phase 5 (可选)

**Phase 5**: Makefile 自动化部署
- 一键导入 Dashboard
- 额度检查命令
- 配置备份和恢复

**预计时间**: 30 分钟
**是否需要**: 当前功能已完整，Phase 5 是锦上添花

---

## 💡 快速命令参考

```bash
# 查看 Indexer 日志
docker logs -f web3-debug-app

# 查看 Prometheus 指标
curl http://localhost:8083/metrics | grep -E "(rpc_quota|token_transfer)"

# 查询 Prometheus API
curl 'http://localhost:9091/api/v1/query?query=rpc_quota_usage_percent' | jq '.'

# 检查容器状态
docker ps --format "table {{.Names}}\t{{.Status}}"

# 重启 Indexer（如果需要）
docker restart web3-debug-app
```

---

**准备就绪！** 🚀

现在打开浏览器访问 **http://localhost:4000**，按照上述 3 步操作导入 Dashboard。

有任何问题，查看 `IMPORT_DASHBOARD_GUIDE.md` 获取详细的故障排查指南。

---

**创建时间**: 2026-02-16 00:12 JST
**维护者**: Claude Code
**状态**: ✅ Phase 4 代码完成，等待用户导入验证
