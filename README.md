Web3 Indexer Dashboard — 项目简介（简洁、便于技术验证）

Live demo: https://demo2.st6160.click/

概述
- 一个全栈容器化的 Web3 索引器示例工程，侧重可观测性与稳定性。实现了 Fetcher / Sequencer / Processor 解耦的流水线设计、RPC 池的自动故障转移、以及面向高频交易场景的 nonce 对齐与状态持久化。工程以可复现的方式提供端到端环境，便于技术人员验证功能与性能指标。

🔐 **加密身份验证 (EdDSA)**
- **开发者:** 周伟 (Zhou Wei) <zhouwei6160@gmail.com>
- **GPG 指纹:** \`FFA0 B998 E7AF 2A9A 9A2C 6177 F965 25FE 5857 5DCF\`
- **验证:** 本仓库使用 Ed25519 密钥进行签名。运行 \`make verify-identity\` 验证代码完整性。

快速启动（最少依赖）
- 前提：目标机器安装了 Docker 与 Docker Compose。
- 克隆并启动示例环境：
  ```
  git clone https://github.com/starwar6160/web3-indexer-go
  cd web3-indexer-go
  make demo
  ```
  make demo 的流程：docker compose down -> docker compose up --build -> stress-test（包含 Anvil 私链、Postgres、Indexer、Dashboard 与压测工具）。

如何验证（建议步骤）
1. 检查容器状态
  ```
  docker compose ps
  docker logs -f web3-indexer-indexer
  ```
2. 发送一笔手动交易（在 anvil 容器内使用 cast）
```
# 进入 anvil 容器手动打一笔钱
docker exec -it web3-indexer-anvil cast send --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --value 1ether 0x70997970C51812dc3A010C7d01b50e0d17dc79C8 --rpc-url http://127.0.0.1:8545
```
  验证点：Indexer 日志应记录该交易并写入数据库；Dashboard（由 docker-compose 暴露的端口）会在近期区块/交易数展示相关变化。

3. 运行或观察压测（make demo 已包含）
  - 观察 TPS、区块高度、索引延迟变化。
  - 在 Postgres 中比对交易条目数：
    ```
    psql -h localhost -p <pg-port> -U <user> -d <db> -c "SELECT COUNT(*) FROM txs;"
    ```
    与链上区块/交易数量进行对应比对，确认数据一致性。

关键实现点（便于验证与代码审查）
- 架构：Fetcher → Sequencer → Processor 三阶段流水线，职责分离，便于扩展与单元测试。
- 非常规场景处理：
  - nonce_drift 自动对齐：处理并发发送时的 nonce 冲突，保证在高负载下索引的一致性。
  - Checkpoint 持久化：关键进度点（例如已处理区块高度/交易指针）持久写入 Postgres，以支持优雅停机与恢复。
- Smart Sleep 模式（节省 RPC 调用）：
  - Active（高性能短时运行）
  - Watching（通过 WebSocket 低频监听，显著降低 RPC 消耗）
  - Idle（无活动时几乎不消耗 RPC 配额）
  验证方式：切换场景并观察 RPC 请求量、Dashboard 状态与日志中模式切换记录。
- RPC 池：支持多节点健康检查与故障切换，验证方法为模拟 RPC 节点下线并观察自动切换与重试行为。
- 连接稳定性：WebSocket 持久连接 + Ping/Pong 心跳，缓解 CDN/代理导致的静默断连问题。可以通过主动断连/代理模拟来验证重连逻辑。
- 并发与资源：基于 Go 的协程池，支持 10+ 并发 worker；运行时内存占用控制在较低范围（工程中目标 <200MB）。可通过容器监控（docker stats / Prometheus 指标）验证。

#### 🛡️ Data Integrity & Security (EdDSA Signing)

To ensure end-to-end data integrity and prevent man-in-the-middle (MITM) attacks or data tampering at the edge (e.g., WAF or Proxy levels), this project implements a **cryptographic provenance layer**:

* **Response Signing:** Every API response is dynamically signed using **Ed25519 (EdDSA)**. Unlike ECDSA, EdDSA provides deterministic signing, eliminating risks associated with poor high-entropy random number generators.
* **Identity Binding:** The signing key is derived from a GnuPG-protected identity, linking the software's execution output directly to the developer's verified cryptographic identity.
* **Verification:** Clients can verify the authenticity of the data by checking the `X-Payload-Signature` header against the public key fingerprint provided in the documentation.
* **Edge Defense:** Integrated with Cloudflare WAF to filter automated bot traffic (User-Agent filtering) and rate-limit high-frequency RPC probing, ensuring high availability of the indexing pipeline.


可观测性与 SRE 实践
- Prometheus 指标 + Dashboard（Vanilla JS）展示 TPS、区块高度、队列长度、RPC 健康等。
- 日志与指标用于定位瓶颈：Fetcher/Sequencer/Processor 的延迟、重试计数与失败率均可在指标中分解查看。
- 可安全暴露内网节点（示例使用 Cloudflare Tunnel 配置），生产部署应注意访问控制与 WAF 规则配置。

技术栈
- Go 1.21+（并发与 Context 管理）
- PostgreSQL（持久化与 Checkpoint）
- Docker / Docker Compose（环境复现）
- Anvil (Foundry) 作本地开发链
- Prometheus + 简易 Web Dashboard（零前端依赖）
- Cloudflare Tunnel（示例的内网穿透/零信任方案）

项目结构（便于定位实现）
web3-indexer-go/
├── cmd/indexer/           # 主程序入口（Service Manager）
├── internal/
│   ├── engine/            # 核心引擎（Fetcher, Sequencer, Processor）
│   ├── rpc_pool/          # 多节点故障转移池与健康检查实现
│   ├── state_manager/     # 状态机与 Checkpoint 持久化
│   └── web/               # WebSocket / Dashboard 后端
├── scripts/               # 数据库初始化与自动化脚本
├── Makefile               # 启动、压测与辅助命令
└── docker-compose.yml     # 基础设施容器化配置

验证提示与常见检查点
- 日志中应有三阶段流水线的处理记录（fetch → seq → process）。
- Postgres 中的表（例如 txs、checkpoints）应随压测产生预期数据量。
- 在模拟 RPC 节点不可用时，RPC 池应自动切换且系统保持可用。
- 模式切换（Active/Watching/Idle）应在指标与日志中可见，并对应 RPC 使用量变化。

联系方式
- 项目仓库：https://github.com/starwar6160/web3-indexer-go
- 欢迎通过仓库 Issue 或 PR 交流具体实现与复现步骤。

若需要，我可以把针对某个验证项（如 nonce_drift 的执行路径、RPC 池的健康检查实现或 Checkpoint 恢复逻辑）抽出具体文件与代码片段，便于逐步审查。