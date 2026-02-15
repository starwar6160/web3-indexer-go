# 🚀 Web3 Indexer - Production-Grade Deployment Guide

本文档描述了 Web3 Indexer 的生产级部署方案，支持**一键部署**和**环境健壮性检测**。

---

## 📋 目录

- [部署模式](#部署模式)
- [环境要求](#环境要求)
- [快速开始](#快速开始)
- [生产部署](#生产部署)
- [混合部署](#混合部署)
- [目录规范](#目录规范)
- [故障排查](#故障排查)

---

## 🎯 部署模式

Web3 Indexer 支持两种生产级部署模式：

### 1. Systemd 部署（推荐生产环境）

**特点：**
- ✅ 原生进程监控与自愈
- ✅ 开机自动启动
- ✅ 集成系统日志（journald）
- ✅ 适合长期运行的服务器

**适用场景：** VPS、专用服务器、云主机

### 2. 混合部署（推荐开发环境）

**特点：**
- ✅ 数据库等基础设施容器化
- ✅ 应用程序直接运行在宿主机
- ✅ 快速迭代和调试
- ✅ 资源占用低

**适用场景：** 本地开发、演示环境、资源受限设备

---

## 🔧 环境要求

### 最低要求

| 组件 | 版本 | 说明 |
|------|------|------|
| **Go** | 1.21+ | 编译和运行 |
| **Docker** | 20.10+ | 容器化基础设施 |
| **PostgreSQL** | 15+ | 数据存储 |
| **系统内存** | 2GB+ | 推荐 4GB+ |
| **磁盘空间** | 20GB+ | 包含数据库和日志 |

### 系统支持

- ✅ Ubuntu 20.04+
- ✅ Debian 11+
- ✅ CentOS 8+
- ✅ macOS 12+ (部分功能)

---

## ⚡ 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/your-org/web3-indexer-go.git
cd web3-indexer-go
```

### 2. 环境检查

```bash
make check-env
```

**输出示例：**
```
🔍 Checking environment dependencies...
✅ All dependencies installed!
go version go1.24.13 linux/amd64
Docker version 28.2.2, build 28.2.2-0ubuntu1~24.04.1
systemd available: ✅
```

### 3. 初始化配置

```bash
make init
```

**输出示例：**
```
🚀 Initializing Web3 Indexer environment...
📝 Creating .env from template...
✅ .env created! Please edit it with your configuration.
✅ Environment initialized!

Next steps:
  1. Edit .env with your configuration
  2. Run 'make demo' to start development environment
  3. Run 'make deploy-service' for production deployment
```

### 4. 编辑配置

```bash
nano .env
```

**关键配置项：**
```bash
# 数据库连接
DATABASE_URL="postgresql://postgres:password@localhost:5432/web3_indexer?sslmode=disable"

# RPC 节点（多个逗号分隔，支持故障转移）
RPC_URLS="https://sepolia.infura.io/v3/YOUR_KEY,https://rpc.sepolia.org"

# 链配置
CHAIN_ID=11155111  # Sepolia testnet
START_BLOCK=0
BATCH_SIZE=100

# 性能调优
MAX_CONCURRENCY=10
POLL_INTERVAL=5s
```

---

## 🚀 生产部署

### 部署流程

```bash
# 1. 环境检查
make check-env

# 2. 编译 + 部署（保留数据）
make deploy-service
```

**部署过程：**

```
🚀 Deploying as systemd service (preserving data)...
📁 Creating production directories...
📝 Installing configuration...
📦 Installing binary...
⚙️  Generating systemd unit file...
🔄 Reloading systemd daemon...
✅ Enabling service...
🚀 Starting service...

✅ Service deployed successfully!

Management commands:
  sudo systemctl status web3-indexer        # Check status
  sudo systemctl stop web3-indexer          # Stop service
  sudo systemctl start web3-indexer         # Start service
  sudo journalctl -u web3-indexer -f        # View logs
  tail -f /var/log/web3-indexer/indexer.log # View application logs
```

### 部署架构

```
Production Server
├── /usr/local/bin/web3-indexer     # 二进制可执行文件（只读）
├── /etc/web3-indexer/
│   └── .env                        # 配置文件（敏感，600 权限）
├── /var/log/web3-indexer/
│   ├── indexer.log                 # 应用日志
│   └── indexer.error.log           # 错误日志
└── /etc/systemd/system/
    └── web3-indexer.service        # Systemd 单元文件
```

### Systemd 服务管理

```bash
# 查看服务状态
sudo systemctl status web3-indexer

# 启动服务
sudo systemctl start web3-indexer

# 停止服务
sudo systemctl stop web3-indexer

# 重启服务
sudo systemctl restart web3-indexer

# 查看实时日志
sudo journalctl -u web3-indexer -f

# 查看启动日志
sudo journalctl -u web3-indexer --since today

# 启用开机自启
sudo systemctl enable web3-indexer

# 禁用开机自启
sudo systemctl disable web3-indexer
```

### Systemd 单元文件

自动生成的单元文件：

```ini
[Unit]
Description=Web3 Indexer Service
After=network.target postgresql.service

[Service]
Type=simple
User=your-username
WorkingDirectory=/etc/web3-indexer
EnvironmentFile=/etc/web3-indexer/.env
ExecStart=/usr/local/bin/web3-indexer
Restart=always
RestartSec=5
StandardOutput=append:/var/log/web3-indexer/indexer.log
StandardError=append:/var/log/web3-indexer/indexer.error.log

[Install]
WantedBy=multi-user.target
```

**特性：**
- ✅ 自动重启（`Restart=always`）
- ✅ 网络后启动（`After=network.target`）
- ✅ 环境变量隔离（`EnvironmentFile`）
- ✅ 日志分离（stdout/stderr 分离）
- ✅ 崩溃恢复（5秒后重启）

---

## 🎮 混合部署

### 使用场景

- 本地开发调试
- 快速原型验证
- 演示环境搭建
- 资源受限设备

### 启动混合环境

```bash
make demo
```

**启动流程：**

```
🎮 Starting Demo Mode (Hybrid Architecture)...
📦 Project: web3-demo
🌉 Docker Gateway: 172.17.0.1
🚀 Starting infrastructure (db, prometheus, grafana)...
⏳ Waiting for database to be ready...
✅ Infrastructure ready
🚀 Starting Web3 Indexer (host binary)...
```

**架构图：**

```
┌─────────────────────────────────────────┐
│         Host Machine                   │
│                                         │
│  ┌──────────────────────┐             │
│  │  Go Binary (Host)    │             │
│  │  - Fast iteration    │             │
│  │  - Easy debugging    │             │
│  └──────────┬───────────┘             │
│             │                          │
│             │ TCP 5432                │
│             ▼                         │
│  ┌───────────────────────┐            │
│  │ Docker Network        │            │
│  │ ┌─────────────────┐  │            │
│  │ │ PostgreSQL      │  │            │
│  │ │ Port: 15432     │  │            │
│  │ └─────────────────┘  │            │
│  │ ┌─────────────────┐  │            │
│  │ │ Prometheus     │  │            │
│  │ │ Port: 9091     │  │            │
│  │ └─────────────────┘  │            │
│  │ ┌─────────────────┐  │            │
│  │ │ Grafana        │  │            │
│  │ │ Port: 4000     │  │            │
│  │ └─────────────────┘  │            │
│  └───────────────────────┘            │
└─────────────────────────────────────────┘
```

**优势：**
- 🚀 快速编译（直接运行 `go run`）
- 🔍 实时调试（直接访问 Go 进程）
- 💾 数据持久化（Docker Volume）
- 📊 内置监控（Prometheus + Grafana）

---

## 📁 目录规范

### 开发环境

```
web3-indexer-go/
├── .env                    # 环境配置（初始化时生成）
├── bin/                    # 编译输出
├── logs/                   # 本地日志
├── cmd/                    # 应用入口
├── internal/               # 核心逻辑
└── docker-compose.yml      # 容器编排
```

### 生产环境

```
/
├── usr/
│   └── local/
│       └── bin/
│           └── web3-indexer         # 二进制可执行文件
├── etc/
│   └── web3-indexer/
│       └── .env                    # 配置文件（权限 600）
├── var/
│   └── log/
│       └── web3-indexer/
│           ├── indexer.log         # 应用日志
│           └── indexer.error.log   # 错误日志
└── etc/systemd/system/
    └── web3-indexer.service       # Systemd 单元文件
```

### 权限规范

```bash
# 二进制文件（可执行）
sudo chmod 755 /usr/local/bin/web3-indexer

# 配置文件（仅所有者可读写）
sudo chmod 600 /etc/web3-indexer/.env

# 日志目录（可写）
sudo chmod 755 /var/log/web3-indexer
sudo chown $USER:$USER /var/log/web3-indexer
```

---

## 🔥 故障排查

### 服务无法启动

**检查：**

```bash
# 1. 检查服务状态
sudo systemctl status web3-indexer

# 2. 查看系统日志
sudo journalctl -u web3-indexer -n 50

# 3. 检查应用日志
tail -f /var/log/web3-indexer/indexer.error.log
```

**常见原因：**
- ❌ 数据库连接失败 → 检查 `DATABASE_URL`
- ❌ RPC 节点不可达 → 检查 `RPC_URLS`
- ❌ 配置文件不存在 → 运行 `make init`

### 数据库连接失败

**检查：**

```bash
# 测试数据库连接
psql $DATABASE_URL -c "SELECT 1;"

# 检查 PostgreSQL 状态
sudo systemctl status postgresql

# 检查端口监听
sudo netstat -tlnp | grep 5432
```

### RPC 请求超时

**解决：**

```bash
# 增加 RPC 超时时间（.env）
RPC_TIMEOUT=60s

# 降低并发度
MAX_CONCURRENCY=5

# 添加更多 RPC 节点
RPC_URLS="node1,node2,node3"
```

### 内存占用过高

**调优：**

```bash
# 降低批处理大小
BATCH_SIZE=50

# 增加轮询间隔
POLL_INTERVAL=10s

# 限制数据库连接池
DB_MAX_OPEN_CONNS=10
DB_MAX_IDLE_CONNS=5
```

---

## 📊 监控和日志

### 应用日志

```bash
# 实时查看
tail -f /var/log/web3-indexer/indexer.log

# 搜索错误
grep ERROR /var/log/web3-indexer/indexer.log

# 统计处理速率
grep "Processed block" /var/log/web3-indexer/indexer.log | wc -l
```

### 系统日志

```bash
# 查看 systemd 日志
sudo journalctl -u web3-indexer -f

# 查看今天的日志
sudo journalctl -u web3-indexer --since today

# 查看最近 100 行
sudo journalctl -u web3-indexer -n 100
```

### 性能监控

访问内置监控面板：

- **Prometheus**: http://localhost:9091
- **Grafana**: http://localhost:4000

**关键指标：**
- `indexer_blocks_processed_total` - 处理区块总数
- `indexer_rpc_requests_duration_seconds` - RPC 请求延迟
- `indexer_db_connections_current` - 数据库连接数

---

## 🛡️ 安全建议

### 生产环境

1. **配置文件权限**
   ```bash
   sudo chmod 600 /etc/web3-indexer/.env
   ```

2. **使用专用用户**
   ```bash
   sudo adduser --system --group web3-indexer
   # 编辑 systemd unit: User=web3-indexer
   ```

3. **防火墙规则**
   ```bash
   # 仅允许本地访问数据库
   sudo ufw deny 5432
   sudo ufw allow from 127.0.0.1 to any port 5432
   ```

4. **定期更新**
   ```bash
   # 更新系统包
   sudo apt update && sudo apt upgrade -y

   # 更新 Go 依赖
   go get -u ./...
   go mod tidy
   ```

5. **日志轮转**
   ```bash
   # 创建 logrotate 配置
   sudo nano /etc/logrotate.d/web3-indexer

   # 内容：
   /var/log/web3-indexer/*.log {
       daily
       rotate 7
       compress
       delaycompress
       missingok
       notifempty
   }
   ```

---

## 🔄 更新和维护

### 更新应用

```bash
# 拉取最新代码
git pull origin main

# 部署新版本（保留数据）
make deploy-service
```

### 数据库迁移

```bash
# 运行迁移
make migrate-up

# 回滚迁移
make migrate-down
```

### 备份和恢复

**备份数据库：**

```bash
# 导出数据库
pg_dump $DATABASE_URL > backup_$(date +%Y%m%d).sql

# 压缩备份
gzip backup_$(date +%Y%m%d).sql
```

**恢复数据库：**

```bash
# 解压备份
gunzip backup_20260214.sql.gz

# 恢复数据库
psql $DATABASE_URL < backup_20260214.sql
```

---

## 🎯 面试展示建议

### 方式 1：演示部署流程

```bash
# 展示环境探测
make check-env

# 展示一键部署
make deploy-service

# 展示服务管理
sudo systemctl status web3-indexer
```

### 方式 2：架构图解释

> "我为项目设计了两套部署架构：
>
> **生产环境**使用原生 **systemd** 实现进程监控与自愈，适合长期运行的服务器；
>
> **开发环境**采用**混合架构**，通过 Docker 快速拉起基础设施（数据库、监控），同时保持 Go 代码直接运行在宿主机，极大提升迭代速度。
>
> 这种对**开发者体验（DX）**的关注，体现了作为资深架构师的思维深度。"

### 方式 3：讲述运维理念

> "我不相信'手工操作'，我相信自动化流程。
>
> 部署流程中集成了**环境探测**、**依赖检测**、**自动生成配置**，确保在不同机器上都能'一把过'。
>
> 这就是我作为工程师对**可维护性**和**健壮性**的承诺。"

---

**最后更新：** 2026-02-14
**维护者：** Web3 Indexer Team
