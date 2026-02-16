# 商业节点配置快速指南

## 🎯 目标

使用商业节点（Alchemy/Infura）替代公共节点，确保 8083 调试容器的稳定性。

---

## 📋 准备工作

### 1. 获取 Alchemy API Key（推荐）

1. 访问 https://www.alchemy.com/
2. 注册账号（免费）
3. 创建新 App → 选择 "Sepolia" 网络
4. 复制 HTTPS 和 WSS URL

**免费额度**：
- ✅ 300M CU/月（约 10M CU/天）
- ✅ 支持批处理请求
- ✅ WebSocket 连接稳定

### 2. 获取 Infura API Key（备用）

1. 访问 https://infura.io/
2. 注册账号（免费）
3. 创建新项目 → 选择 "Sepolia" 网络
4. 复制 HTTPS 和 WSS URL

**免费额度**：
- ✅ 500k CU/天
- ✅ 适合作为备份节点

---

## 🔧 配置步骤

### Step 1: 编辑环境变量文件

```bash
# 编辑 .env.debug.commercial
vim .env.debug.commercial
```

替换以下内容：
```bash
ALCHEMY_SEPOLIA_HTTPS=https://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY
ALCHEMY_SEPOLIA_WSS=wss://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY
INFURA_SEPOLIA_HTTPS=https://sepolia.infura.io/v3/YOUR_INFURA_KEY
INFURA_SEPOLIA_WSS=wss://wss.sepolia.infura.io/ws/v3/YOUR_INFURA_KEY
```

### Step 2: 重启调试容器

```bash
# 停止当前容器
docker-compose -f docker-compose.debug.yml down

# 使用商业节点配置启动
docker-compose -f docker-compose.debug.yml --env-file .env.debug.commercial up -d --build
```

### Step 3: 验证连接

```bash
# 查看启动日志
docker logs -f web3-debug-app | grep -E "(RPC|Token filtering|Enhanced)"

# 期望输出：
# ✅ Token filtering enabled with defaults
# Enhanced RPC Pool initialized with 2/2 nodes healthy
```

---

## 📊 CU 消耗估算

### 使用服务端过滤后

**请求模式**：
- 每 15 秒执行一次 `eth_getLogs`（跨度 1000 块）
- 每次请求消耗：~15-20 CU
- 每天请求次数：24 * 60 * 4 = 5,760 次

**每日 CU 消耗**：
```
5,760 次 * 20 CU = 115,200 CU/天
```

**结论**：
- ✅ Alchemy 免费版（10M CU/天）：**仅消耗 1.15%**
- ✅ Infura 免费版（500k CU/天）：**仅消耗 23%**

**即使 24 小时运行，额度也绰绰有余！**

---

## 🛡️ 故障转移机制

SmartClient 会自动在节点间切换：

```
Primary: Alchemy (优先使用)
  ↓ (故障)
Backup: Infura (自动切换)
  ↓ (故障)
Retry: 指数退避 (1s → 2s → 4s → ... → 60s)
```

**日志示例**：
```
RPC node https://eth-sepolia.g.alchemy.com/... marked unhealthy
Failover to backup node: https://sepolia.infura.io/v3/...
```

---

## ⚠️ 注意事项

### 1. WebSocket 连接数限制

商业节点通常限制 WSS 连接数（如 Alchemy 限制 2 个并发）。

**当前环境**：
- 8081 (web3-testnet-app) - 可能使用 WSS
- 8082 (web3-demo2-app) - 本地 Anvil，不使用 WSS
- 8083 (web3-debug-app) - 建议使用 HTTPS 轮询（不使用 WSS）

**配置建议**：
```bash
# 8083 容器不设置 WSS_URL（仅使用 HTTPS 轮询）
WSS_URL=
```

### 2. Rate Limiting (429 错误)

即使 CU 没用完，频繁请求也可能触发 429。

**防护措施**：
- ✅ `RPC_RATE_LIMIT=5`（每秒最多 5 个请求）
- ✅ `FETCH_CONCURRENCY=1`（单并发）
- ✅ `MAX_SYNC_BATCH=5`（小批次）

### 3. 成本监控

**Alchemy Dashboard**：
- 查看 CU 使用情况
- 监控请求成功率
- 设置告警阈值（如 80% 额度使用）

---

## 🔍 验证清单

启动后，检查以下内容：

- [ ] 日志显示 "Enhanced RPC Pool initialized with 2/2 nodes healthy"
- [ ] 日志显示 "✅ Token filtering enabled with defaults"
- [ ] 无 521 或 404 错误
- [ ] 数据库中只有 4 种不同的 `token_address`
- [ ] 演示界面显示 USDC/DAI/WETH/UNI 的转账记录

---

## 📈 性能对比

| 指标 | 公共节点 | 商业节点 |
|------|----------|----------|
| 稳定性 | ⚠️ 经常宕机 | ✅ 99.9% 可用 |
| 速度 | ⚠️ 1-5 秒延迟 | ✅ < 500ms |
| 限流 | ⚠️ 频繁 429 | ✅ 宽松额度 |
| 成本 | ✅ 免费 | ✅ 免费版足够 |
| 数据质量 | ✅ 相同 | ✅ 相同 |

---

## 🎉 完成后

1. **验证 8083 端口可访问**
   ```bash
   curl http://localhost:8083/api/status | jq '.'
   ```

2. **添加到 Cloudflare Tunnel**（调试完成后）
   ```yaml
   # Cloudflare Tunnel 配置
   - service: http://localhost:8083
     hostname: demo3.example.com
   ```

3. **监控 CU 消耗**
   - Alchemy Dashboard: https://www.alchemy.com/
   - 设置每日 CU 使用告警

---

**最后更新**: 2026-02-16
**维护者**: Claude Code
