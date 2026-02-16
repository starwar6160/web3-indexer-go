# Debug 环境设置指南

## 🎯 当前环境状态

| 端口 | 容器名 | 状态 | 用途 | Cloudflare 子域名 |
|------|--------|------|------|-------------------|
| 8081 | web3-testnet-app | ✅ 运行中 | Sepolia 测试网（原配置） | demo1 |
| 8082 | web3-demo2-app | ✅ 运行中 | Anvil 本地演示 | demo2 |
| 8083 | web3-debug-app | ⚠️ RPC 故障 | 代币过滤调试环境 | （待公开）|

---

## ✅ 代币过滤功能已启用

**日志确认**：
```
✅ Token filtering enabled with defaults
🎯 Fetcher configured to watch hot tokens only
   - watched_count: 4
   - bandwidth_saving: ~98%
   - demo_experience: meaningful_transfers_only
```

**监控的热门代币**：
- USDC (`0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238`)
- DAI (`0xff34b3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357`)
- WETH (`0x7b79995e5f793A07Bc00c21412e50Ecae098E7f9`)
- UNI (`0xa3382DfFcA847B84592C05AB05937aE1A38623BC`)

---

## ⚠️ 公共 RPC 节点问题

当前所有 3 个公共节点都不可用：
- `rpc.sepolia.org` - 超时
- `sepolia.publicnode.com` - 404 Not Found
- `ethereum-sepolia.blockpi.network` - 521 服务器宕机

**这是公共节点的常见问题**，建议使用付费 RPC 节点（Alchemy, Infura）。

---

## 🔧 快速修复方案

### 方案 1: 使用备份公共节点（推荐用于快速测试）

```bash
# 停止当前容器
docker-compose -f docker-compose.debug.yml down

# 使用备份 RPC 节点启动
export DEBUG_RPC_URLS="https://rpc.ankr.com/eth_sepolia,https://endpoints.omniatech.io/v1/eth/sepolia/public,https://eth-sepolia.public.blastapi.io"
docker-compose -f docker-compose.debug.yml up -d --build
```

### 方案 2: 使用付费节点（推荐用于生产环境）

编辑 `.env.debug.local`（创建此文件）：
```bash
DEBUG_RPC_URLS=https://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY,https://sepolia.infura.io/v3/YOUR_INFURA_KEY
```

然后启动：
```bash
docker-compose -f docker-compose.debug.yml --env-file .env.debug.local up -d --build
```

---

## 📁 配置文件

### 主要配置文件

1. **`docker-compose.debug.yml`** - Debug 容器配置
2. **`.env.debug.backup`** - 备份 RPC URL 配置
3. **`TOKEN_FILTERING_IMPLEMENTATION.md`** - 代币过滤实施文档

### 关键配置参数

```yaml
environment:
  - TOKEN_FILTER_MODE=whitelist        # 启用代币过滤
  - WATCHED_TOKEN_ADDRESSES=           # 留空 = 使用默认 4 个热门代币
  - RPC_RATE_LIMIT=3                   # 每秒 3 个请求
  - MAX_SYNC_BATCH=3                   # 每次最多同步 3 个块
  - FETCH_CONCURRENCY=1                # 单并发
  - PORT=8083                          # 主机端口
```

---

## 🚀 常用命令

### 查看日志
```bash
# 实时日志
docker logs -f web3-debug-app

# 查看代币过滤相关日志
docker logs web3-debug-app 2>&1 | grep -E "(Token filtering|watched|Fetcher configured)"

# 查看 RPC 健康状态
docker logs web3-debug-app 2>&1 | grep -E "(Health check|RPC node|healthy)"
```

### 重启容器
```bash
docker-compose -f docker-compose.debug.yml restart
```

### 重新构建（代码更新后）
```bash
docker-compose -f docker-compose.debug.yml up -d --build
```

### 验证数据库
```bash
# 查看不同的代币地址数量（应该是 4 个）
docker exec web3-testnet-db psql -U postgres -d web3_sepolia \
  -c "SELECT COUNT(DISTINCT token_address) FROM transfers;"

# 查看最近的转账记录
docker exec web3-testnet-db psql -U postgres -d web3_sepolia \
  -c "SELECT token_address, COUNT(*) as count FROM transfers GROUP BY token_address ORDER BY count DESC LIMIT 10;"
```

---

## 🌐 访问地址

- **本地访问**: http://localhost:8083
- **API 状态**: http://localhost:8083/api/status
- **Prometheus 指标**: http://localhost:8083/metrics

---

## 📊 验证清单

启动后，检查以下内容：

- [ ] 启动日志显示 "✅ Token filtering enabled with defaults"
- [ ] 日志中看到监控的 4 个代币地址
- [ ] RPC 节点健康检查通过（至少 1 个节点在线）
- [ ] 数据库中只有 4 种不同的 `token_address`
- [ ] 演示界面显示 USDC/DAI/WETH/UNI 的转账记录
- [ ] 人眼感觉数据在快速刷新（每秒约 1-3 条）

---

## 🎉 成功后

### 1. 添加到 Cloudflare Tunnel

编辑 Cloudflare Tunnel 配置，添加 8083 端口映射：
```yaml
# example.com 配置
- service: http://localhost:8083
  hostname: demo3.example.com  # 或其他子域名
```

### 2. 验证公开访问

访问 `https://demo3.example.com`，确认：
- 界面正常显示
- 数据实时更新
- 代币过滤生效（只显示热门代币）

### 3. 监控和维护

- 定期检查 RPC 节点健康状态
- 监控数据库增长速度（应该比全量索引慢 95%+）
- 观察演示界面数据刷新频率

---

**最后更新**: 2026-02-16
**维护者**: Claude Code
