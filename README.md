# 🚀 Web3 Indexer Dashboard

**Live Demo:** [https://demo2.st6160.click/](https://demo2.st6160.click/)

> **Engineering Philosophy**: 20 年后端架构经验沉淀，专注于 Web3 基础设施的稳定性（SRE 级可靠性）与可观测性。本项目展示了如何将传统高性能分布式系统设计（Fetcher/Sequencer 解耦、流水线架构）与区块链技术栈深度融合。

---

## 🛠️ Quick Start (3 Minutes)

为了方便面试官/猎头快速复现，本项目支持一键启动完整的端到端环境（包含 Anvil 私有链、Postgres 数据库及高频交易压测）：

```bash
git clone https://github.com/your-repo/web3-indexer-go
cd web3-indexer-go

# 一键启动演示流水线 (重置环境 + 实时产块 + 高频交易模拟)
make demo 
```

*`make demo` 内部集成了：`clean-env` -> `docker-compose up` -> `db-migrate` -> `indexer-start` -> `stress-test`.*

---

## 🧠 核心研发要点 (Key Insights)

* **状态自愈与高吞吐**：针对高频转账（TPS ~50）场景，实现了基于 `Fetcher-Sequencer-Processor` 的解耦流水线架构。内置 `nonce_drift` 自动对齐逻辑，确保在高负载下索引不中断且数据 100% 一致。
* **智能休眠系统 (Smart Sleep)**：
    - **Active Mode**: 5分钟高性能演示模式。
    - **Idle Mode**: 无活动时自动进入，RPC 消耗降至 0。
    - **Watching Mode**: 通过 WebSocket 监听实现极低成本实时监控（97% 成本节省）。
* **SRE 级防御与穿透**：通过 Cloudflare Tunnel 实现零信任内网穿透，将内网物理节点（MiniPC）安全暴露至公网。配置边缘 WAF 规则，精准拦截恶意爬虫，保障服务在公网的稳定性。
* **高可用 RPC 池**：内置多节点故障转移机制，支持 RPC 节点自动健康检查与切换，确保 99.9% 的可用性。
* **实时可观测性**：基于 WebSocket 的持久连接，内置 Ping/Pong 心跳机制，有效应对 CDN 代理导致的静默断连，Dashboard 实时展示 TPS、区块高度及系统状态。

---

## ✨ 核心特性

### 🎯 成本优化
| 模式 | RPC配额使用 | 节省比例 | 适用场景 |
|------|-------------|----------|----------|
| **Active** | ~2.4M credits/day | 0% | 演示模式 |
| **Idle** | ~0 credits/day | **100%** | 长期休眠 |
| **Watching** | ~0.1M credits/day | **97%** | 低功耗监控 |

### 🛡️ 稳定性保障
- **优雅停机**: 完整 Checkpoint 持久化，数据零丢失。
- **并发性能**: 基于 Go Coroutine 池，支持 10+ 并发 Worker。
- **内存优化**: 运行时内存占用 < 200MB。

---

## ⚙️ 技术栈

- **Language**: Go 1.21+ (Concurrency, Context management)
- **Persistence**: PostgreSQL
- **Infrastructure**: Docker & Docker Compose
- **Dev Chain**: Anvil (Foundry)
- **Monitoring**: Prometheus & Web Dashboard (Vanilla JS/CSS for zero-dependency)
- **Deployment**: Cloudflare Tunnel (Zero Trust Architecture)

---

## 📂 项目结构

```
web3-indexer-go/
├── cmd/indexer/           # 主程序入口 (Service Manager)
├── internal/
│   ├── engine/           # 核心引擎 (Fetcher, Sequencer, Processor)
│   ├── rpc_pool/         # 多节点故障转移池
│   ├── state_manager/    # 智能状态转换机
│   └── web/              # WebSocket Dashboard
├── scripts/              # 数据库初始化与自动化脚本
├── Makefile              # 工业级控制台
└── docker-compose.yml    # 基础设施容器化配置
```

---

## 🤝 Contact & Feedback

本项目由 [您的名字] 开发。如果您对高性能索引器架构或 Web3 基础设施感兴趣，欢迎交流。

**🚀 立即开始: `make demo`**