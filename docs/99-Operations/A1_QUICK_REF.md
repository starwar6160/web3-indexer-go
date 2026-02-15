# Web3 Indexer - Testnet 迁移快速参考卡

> **小步快跑 · 原子化验证 · 环境隔离**

---

## 🚀 快速启动（3 步）

```bash
# 1️⃣ 预检（5 步原子化验证）
make a1-pre-flight

# 2️⃣ 启动测试网索引器
make a1

# 3️⃣ 查看日志
docker logs -f web3-indexer-sepolia-app
```

---

## ✅ 验证清单

### 启动前
- [ ] `.env.testnet.local` 已配置 API Key
- [ ] `make a1-pre-flight` 全部通过
- [ ] `.env.testnet` 中 `START_BLOCK=latest`

### 启动后
- [ ] 起始块显示 `1026xxxx` 而非 `#1`
- [ ] 日志处理间隔约 1 秒（QPS=1）
- [ ] Dashboard 可访问：`http://localhost:8081`
- [ ] E2E Latency < 60 秒

---

## 🔧 常用命令

| 操作 | 命令 |
|------|------|
| **预检** | `make a1-pre-flight` |
| **启动** | `make a1` |
| **日志** | `make logs-testnet` |
| **重置** | `make reset-a1` |
| **停止** | `make stop-testnet` |
| **状态** | `docker ps \| grep web3-testnet` |

---

## 📊 关键端点

| 端点 | URL |
|------|-----|
| **Dashboard** | http://localhost:8081 |
| **Metrics** | http://localhost:8081/metrics |
| **API Status** | http://localhost:8081/api/status |

---

## 🔍 故障排查速查

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| RPC 连接失败 | API Key 错误 | 检查 `.env.testnet.local` |
| 从 #0 开始 | `START_BLOCK` 非 latest | 运行 `make reset-a1` |
| 429 错误 | QPS 过高 | 降低 `RPC_RATE_LIMIT=0.5` |
| E2E 爆表 | 从创世块同步 | 确认 `START_BLOCK=latest` |

---

## 📝 配置对照

| 参数 | Demo | Testnet |
|------|------|---------|
| **DB Name** | `web3_indexer` | `web3_sepolia` |
| **DB Port** | `15432` | `15433` |
| **API Port** | `8080` | `8081` |
| **Chain ID** | `31337` | `11155111` |
| **Start Block** | `0` | `latest` |
| **QPS** | `200` | `1` |

---

## 💡 核心原则

1. **小步验证**：每步独立可测，失败快速定位
2. **环境隔离**：Docker Project Name 实现容器级隔离
3. **保守限流**：QPS=1 确保不被封禁
4. **可观测性**：Metrics + Dashboard 双重监控

---

**完整文档**：`docs/A1_VERIFICATION_GUIDE.md`
