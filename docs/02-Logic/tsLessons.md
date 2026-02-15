# Web3 Indexer 工业级迁移与调试经验总结 (V2)

> **环境**: Ubuntu 5600U / Docker / Go 1.24 / Sepolia Testnet
> **日期**: 2026-02-15
> **核心目标**: 实现从仿真(Emulator)到实战(Sepolia)的无缝切换，并建立 6 个 9 持久性逻辑。

---

## 1. 开发工作流优化：Air 热重载 vs. Docker 容器
### 痛点
频繁运行 `make a1` (docker-compose build) 耗时 30s+，严重打断开发心流。
### 经验
- **分级开发模式**:
    - **UI/API 逻辑**: 使用 `make air` 在宿主机直接运行。
    - **基础设施**: 数据库、Grafana、Prometheus 保持在 Docker 中常驻。
- **环境自愈**: 在 `Makefile` 中集成 `air` 的自动安装与端口占用检查，实现一键切换。
- **Volume 挂载**: 对于监控配置（Grafana JSON/Prometheus YAML），通过 Volume 挂载实现修改即生效，无需重启容器。

## 2. 数据逻辑守卫 (Data Logic Guards)
### 案例分析：哈希自指 (Hash Self-Reference) Bug
- **现象**: 看板显示 Block 的 Hash 和 Parent Hash 完全一致。
- **根因**: 
    1. 前端 UI 渲染时存在 `(parent_hash || hash)` 的错误 fallback 逻辑。
    2. 后端解析历史区块时，由于处理速度极快，产生了时空错位的视觉假象。
- **工业级解决方案**:
    - **源头拦截**: 在 `Processor` 写入数据库前增加 `FATAL` 级校验：`if hash == parent_hash { return err }`。
    - **显式查询**: API SQL 语句放弃 `SELECT *`，明确指定列顺序，防止字段自动映射错误。

## 3. 监控“幽灵化”与心跳降级逻辑
### 现象
在节能模式或 RPC 限流时，Dashboard 的 `Latest Block` 会显示为 `0`。
### 经验
- **Background Heartbeat (心跳缓存)**:
    - 无论索引器是否工作，由一个独立协程每 15s 更新数据库中的 `sync_checkpoints`。
    - API 接口实现“缓存降级”：优先获取实时高度，若失败则从 `sync_checkpoints` 读取最近一次缓存。
- **E2E Latency 的动态定义**:
    - **追赶模式**: 延迟 > 100 块时，显示 `Catching up... (N blocks)`。
    - **实时模式**: 延迟 < 100 块时，计算 `time.Since(processed_at)`，输出 `%.2fs` 格式。

## 4. 成本与性能的平衡：节能模式开关
### 架构设计
- 引入 `ENABLE_ENERGY_SAVING` 开关。
- **演示模式**: 访客触发（唤醒 3min，冷却 3min），极大节省 Alchemy/Infura 的免费额度。
- **24/7 模式**: 5600U 挂机专用，持续同步，确保数据 100% 连续性。

## 5. 网络拓扑：跨容器监控
### 经验
- 当应用跑在宿主机 (air) 而监控跑在 Docker 时，Prometheus 必须通过宿主机网关 (如 `172.17.0.1`) 抓取指标。
- 最终生产部署时，通过 Docker Network 别名 (如 `sepolia-app:8080`) 实现服务发现，避免硬编码 IP。

---

**总结**: 优秀的后端系统不仅在于代码写得快，更在于其**可观测性(Observability)**、**容错性(Resilience)**以及**极速的反馈循环(Feedback Loop)**。
