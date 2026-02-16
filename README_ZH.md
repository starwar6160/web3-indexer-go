# 工业级 Web3 事件索引器 (横滨实验室)

[🌐 **English**](./README.md) | [🏮 **中文说明**](./README_ZH.md) | [🗾 **日本語の説明**](./README_JA.md)

基于 **Go**、**PostgreSQL** 和 **Docker** 构建的高可靠、低成本以太坊事件索引器。专为高性能数据摄取设计，同时严格遵守商用 RPC 的配额限制。本项目在横滨硬件实验室完成开发与压测。

在线演示: [https://demo2.st6160.click/](https://demo2.st6160.click/)

## 🚀 技术亮点

*   **工业级可靠性**：支持 24/7 无人值守运行，采用 **蓝绿部署 (Staging-to-Production)** 工作流，实现秒级热更新（离线窗口 < 2s）。
*   **成本导向架构**：集成 **加权令牌桶 (Weighted Token Bucket)** 限流器，最大化 Alchemy/Infura 免费档配额利用率（主备权重 3:1）。
*   **429 自动熔断**：智能错误检测，一旦收到限流错误即触发 5 分钟冷却期并自动切换流量。
*   **确定性安全守卫**：启动时强制执行 **NetworkID/ChainID 校验**，物理杜绝跨环境数据库污染。
*   **高效范围抓取**：优化 `eth_getLogs` 批量处理（50 块/请求），配合 **Keep-alive** 进度机制，确保在无事件期间 UI 依然实时更新。
*   **Early-Bird API 模式**：Web 服务启动与引擎初始化解耦，毫秒级开启监听端口，彻底消除容器重启时的 Cloudflare 502 报错。

## 🛠️ 技术栈与实验室环境

*   **后端**: Go (Golang) + `go-ethereum`
*   **基础设施**: Docker (Demo/Sepolia/Debug 环境物理隔离)
*   **存储**: PostgreSQL (每个实例拥有独立物理数据库)
*   **可观测性**: Prometheus + Grafana (多环境一键切换监控面板)
*   **实验室硬件**: AMD Ryzen 7 3800X (8C/16T), 128GB DDR4 RAM, Samsung 990 PRO 4TB NVMe

## 📦 部署工作流

我们采用“测试驱动晋升”流程，确保生产环境绝对稳定：

1.  **测试 (Test)**：通过 `make test-a1` (Sepolia) 或 `make test-a2` (Anvil) 部署到 Staging 端口 (8091/8092)。
2.  **验证 (Verify)**：在 Staging 端口进行冒烟测试。
3.  **晋升 (Promote)**：通过 `make a1` 或 `make a2` 将镜像瞬间平移至生产端口 (8081/8082)。
    *   *原理*：`docker tag :latest -> :stable` + `docker compose up -d --no-build`。

## 📈 性能与优化指标

| 模式 | 目标网络 | RPS 限制 | 延迟 | 策略 |
| :--- | :--- | :--- | :--- | :--- |
| **稳定版 (Stable)** | Sepolia (测试网) | 3.5 RPS | ~12s | 加权多源 RPC |
| **演示版 (Demo)** | Anvil (本地) | 10000+ RPS | < 1s | 零限流模式 |
| **调试版 (Debug)** | Sepolia (测试网) | 5.0 RPS | ~12s | 直连商用 RPC |

## 🔐 加密身份验证

*   **开发者**: 周伟 (Zhou Wei) <zhouwei6160@gmail.com>
*   **实验室位置**: 日本横滨 (Yokohama, Japan)
*   **GPG 指纹**: `FFA0 B998 E7AF 2A9A 9A2C  6177 F965 25FE 5857 5DCF`
*   **验证**: 所有 API 响应均使用 **Ed25519** 进行签名，确保端到端的数据完整性。

---
© 2026 Zhou Wei. Yokohama Lab. All rights reserved.
