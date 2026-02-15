# ⚡ Anvil Quick Start - 5 分钟快速开始

## 一键启动

```bash
# 启动 Anvil 演示环境（自动部署合约 + 发送测试交易）
make demo

# 在另一个终端启动索引器
DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable \
RPC_URLS=http://localhost:8545 \
CHAIN_ID=31337 \
START_BLOCK=0 \
LOG_LEVEL=debug \
./bin/indexer

# 打开浏览器查看 Dashboard
open http://localhost:8080

# 停止所有服务
make anvil-down
```

## 核心验证点

### 1️⃣ RPC 连接
```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'
# 预期: {"jsonrpc":"2.0","result":"0x7a69","id":1}
```

### 2️⃣ 合约部署
```bash
make demo-deploy
# 预期: ✅ Contract deployed at: 0x...
```

### 3️⃣ Sequencer 初始化
启动索引器后，查看日志中是否有：
```
✅ sequencer_started - mode: ordered_processing
```

### 4️⃣ 健康检查
```bash
curl http://localhost:8080/healthz | jq .
# 预期: "status": "healthy"
```

## 常见问题速解

| 问题 | 原因 | 解决 |
|------|------|------|
| `sequencer not initialized` | Sequencer 初始化失败 | 检查日志中 `sequencer_started` 是否出现 |
| `rpc_pool_init_failed` | RPC 连接失败 | 运行 `make anvil-up` 并检查 Anvil 是否运行 |
| `database_connection_failed` | PostgreSQL 连接失败 | 运行 `make anvil-up` 并检查数据库配置 |
| Sequencer buffer 增长 | 前面区块未处理 | 检查 Fetcher 日志中的 `blocks_scheduled` |

## 文件结构

```
web3-indexer-go/
├── cmd/
│   ├── indexer/main.go          # 主程序入口
│   └── demo/deploy.go           # ✨ 新增：演示合约部署脚本
├── internal/engine/
│   ├── sequencer.go             # Sequencer 核心逻辑
│   ├── fetcher.go               # Fetcher 并发抓取
│   ├── processor.go             # Processor 数据库写入
│   └── rpc_pool.go              # RPC 池管理
├── Makefile                     # ✨ 新增：Anvil 测试命令
├── ANVIL_TESTING.md             # ✨ 新增：详细测试指南
├── ANVIL_QUICK_START.md         # ✨ 新增：快速开始指南
└── scripts/anvil-test.sh        # ✨ 新增：自动化测试脚本
```

## 新增 Makefile 命令

```bash
make anvil-up           # 启动 Anvil + PostgreSQL
make anvil-down         # 停止 Anvil + PostgreSQL
make demo-deploy        # 部署演示合约
make demo               # 完整演示（启动 + 部署 + 提示）
make test-anvil         # 运行集成测试
make verify             # 快速验证（30 秒运行）
```

## 演示流程（面试用）

```bash
# 1. 启动本地环境
make demo

# 2. 启动索引器（在另一个终端）
./bin/indexer

# 3. 打开 Dashboard
open http://localhost:8080

# 4. 观察：
#    - Dashboard 显示实时状态
#    - 日志显示区块处理
#    - 健康检查显示所有组件就绪

# 5. 演讲稿：
# "我使用 Anvil 本地模拟链来验证 Go 索引器的核心逻辑。
#  这样做有三个优势：
#  1. 完全控制 - 所有数据都是可预制的
#  2. 快速反馈 - RPC 延迟 < 1ms
#  3. 可重现 - 每次运行都是相同的环境
#  
#  一旦核心逻辑在本地通过验证，切换到 Sepolia 只需修改一个环境变量。"
```

## 关键指标

| 指标 | 本地 Anvil | Sepolia |
|------|-----------|---------|
| RPC 延迟 | < 1ms | 100-500ms |
| 限流风险 | ❌ 无 | ⚠️ 有 |
| 数据可控性 | ✅ 100% | ❌ 0% |
| 调试难度 | ✅ 简单 | ❌ 困难 |
| 成本 | ✅ 免费 | ⚠️ 需要 API Key |

---

**🎯 下一步**: 运行 `make demo` 启动完整演示！
