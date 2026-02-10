# CLAUDE.md - Web3 Indexer Go

## 项目概述

这是一个生产就绪的以太坊区块链索引器，用于索引 ERC20 转账事件。项目采用流水线架构，支持有序处理、多RPC故障转移和高级重组处理。

## 常用开发命令

### 构建和运行
```bash
# 开发环境热重载 (推荐)
air

# 生产级编译与部署
./scripts/publish.sh
./scripts/deploy-prod.sh

# 环境重置 (演示专用)
./scripts/clean-env.sh

# 构建
make build
# 或者
go build -o indexer ./cmd/indexer/main.go

# 运行
make run
# 或者
go run ./cmd/indexer/main.go

# 测试
make test

# 运行特定测试
go test ./internal/engine/...
```

### 数据库迁移
```bash
# 运行迁移
make migrate-up

# 回滚迁移
make migrate-down

# 开发环境设置（PostgreSQL + 迁移）
make dev-setup
```

### Docker 环境
```bash
# 启动 PostgreSQL
make docker-up

# 停止 PostgreSQL
make docker-down

# 查看 PostgreSQL 日志
make docker-logs
```

## 高层架构

### 三阶段流水线架构

```
Fetcher（并发获取） → Sequencer（有序处理） → Processor（数据库写入）
```

**为什么有序处理至关重要？**
- 区块链重组检测需要正确的父哈希验证
- 数据一致性要求交易按区块顺序处理
- 避免重复处理和数据不一致

**核心组件：**

1. **Fetcher** (`internal/engine/fetcher.go`)
   - 并发区块获取，速率限制（默认 100 req/s，200 突发）
   - 多 RPC 节点池，自动故障转移
   - 支持暂停/恢复机制处理重组
   - 使用 Token Bucket 算法进行速率限制

2. **Sequencer** (`internal/engine/sequencer.go`)
   - 确保区块按严格顺序处理
   - 缓冲乱序区块
   - 管理检查点和故障恢复
   - 通知处理器重组事件

3. **Processor** (`internal/engine/processor.go`)
   - 批量处理数据库写入
   - 高级重组处理（浅层和深层）
   - ACID 事务支持
   - Prometheus 指标集成

## 关键架构模式

### 1. Worker Pool 模式
- Fetcher 使用 goroutine 池并发获取区块
- 可配置的工作线程数量（默认 10）
- 通过 `sync.WaitGroup` 协调工作

### 2. Token Bucket 速率限制
- 每个RPC节点独立的速率限制器
- 可配置的速率（req/s）和突发大小
- 防止API限制和节点封禁

### 3. ACID 事务
- 所有数据库写入在事务中执行
- 事务包含：区块插入、转移事件、检查点更新
- 失败时自动回滚

### 4. 父哈希验证
- 重组检测不仅依赖区块号，更依赖父哈希
- 比较预期父哈希和实际父哈希
- 检测分支切换（reorg）

## 重要文件路径

### 核心文件
- `cmd/indexer/main.go` - 入口点和协调逻辑
- `internal/engine/fetcher.go` - 并发区块获取，速率限制
- `internal/engine/sequencer.go` - 有序区块处理
- `internal/engine/processor.go` - 区块处理和重组处理
- `internal/engine/rpc_pool.go` - 多RPC故障转移
- `internal/models/types.go` - Uint256 类型，支持科学计数法
- `migrations/001_init.sql` - 数据库模式

### 配置和工具
- `.env.example` - 环境变量模板
- `Makefile` - 构建和部署自动化
- `docker-compose.yml` - PostgreSQL 容器设置
- `SECURITY.md` - 安全策略

## 非明显的实现细节

### 1. 重组检测机制
- 通过父哈希比较检测重组（不仅仅是区块号）
- Sequencer 通知 Processor 重组事件
- Processor 根据重组深度执行不同策略

### 2. PostgreSQL CASCADE 删除
- 块表中的级联删除会自动清理相关转账记录
- 确保数据一致性，避免孤立记录

### 3. 科学计数法处理
- NUMERIC 类型需要特殊处理（如 "1.5E+18"）
- `Uint256` 类型实现了 `Valuer`/`Scanner` 接口
- 自动处理 Go big.Int 和 PostgreSQL NUMERIC 的转换

### 4. 暂停/恢复机制
- 使用 `sync.Cond` 而不是 channel 实现暂停/恢复
- 避免复杂的 channel 管理
- 支持全局暂停处理重组

### 5. QuickNode 限制
- `eth_getLogs` API 限制为 2000 个区块
- 自动将大范围查询拆分为多个小查询
- 避免API超时和错误

### 6. 事务检查点
- 检查点更新与区块插入在同一事务中
- 确保崩溃后可以从正确位置恢复
- 使用批处理提高性能

## 配置说明

### 环境变量（来自 .env.example）

```bash
# 数据库
DB_URL="postgresql://user:password@localhost:5432/indexer?sslmode=disable"

# RPC 节点（逗号分隔，支持故障转移）
RPC_URLS="https://sepolia.infura.io/v3/YOUR_KEY,https://api-sepolia.base.org"

# 链ID
# 1 = 主网
# 11155111 = Sepolia
CHAIN_ID=11155111

# 同步配置
START_BLOCK=0
BATCH_SIZE=100
POLL_INTERVAL=5s

# 速率限制
MAX_CONCURRENCY=10
RPC_TIMEOUT=30s

# 微重组保护
CONFIRMATION_DEPTH=6
```

### 多 RPC 故障转移
- 逗号分隔多个 RPC URL
- 自动轮询和故障转移
- 每个节点独立的速率限制

### 链 ID 支持
- `1`: Ethereum Mainnet
- `11155111`: Sepolia Testnet
- 其他 EVM 兼容链可通过环境变量配置

## 测试指南

### 测试文件位置
```
internal/engine/
├── fetcher_test.go     # 获取器单元测试
├── sequencer_test.go   # 排序器单元测试
├── processor_test.go   # 处理器单元测试
└── rpc_pool_test.go    # RPC池单元测试
```
> 注意：健康检查测试可能内嵌在各个组件的测试中

### 运行测试
```bash
# 所有测试
make test

# 特定包测试
go test ./internal/engine/...

# 带覆盖率
go test -cover ./internal/engine/...

# 集成测试
# go test -tags=integration ./...  # 如需集成测试，请先添加 integration build tag
```

### 测试要点
- 单元测试覆盖所有核心组件
- 集成测试验证端到端流程
- 重组场景测试验证数据一致性
- 性能测试验证高吞吐量

## 部署注意事项

### 生产环境
- 使用多个 RPC 节点确保高可用
- 设置合适的确认深度（主网建议 12+，测试网 6）
- 配置足够的数据库连接池
- 启用监控和日志聚合

### Docker 部署
```bash
# 构建镜像
docker build -t web3-indexer .

# 运行容器
docker run -d \
  --name indexer \
  -e DB_URL="postgresql://..." \
  -e RPC_URLS="..." \
  -p 8080:8080 \
  web3-indexer
```

## 监控和指标

### 暴露的端点
- `/metrics` - Prometheus 指标
- `/healthz` - 健康检查
- `/ready` - 就绪检查

### 关键指标
- 区块处理速率
- RPC 节点健康状态
- 数据库连接池状态
- 重组事件计数
- 处理延迟

## 故障排查

### 常见问题
1. **RPC 限制**：增加 `CONFIRMATION_DEPTH` 或降低 `MAX_CONCURRENCY`
2. **数据库连接**：检查连接池配置和网络连通性
3. **重组处理**：监控重组指标，调整确认深度
4. **内存使用**：调整批处理大小，使用批量插入

### 日志级别
```bash
# 开发环境
LOG_LEVEL=debug

# 生产环境
LOG_LEVEL=info
```

## 安全考虑

- 所有敏感信息通过环境变量传递
- 使用 HTTPS 进行 RPC 连接
- 定期轮换 API 密钥
- 实施访问控制和速率限制
- 定期安全扫描

## 贡献指南

1. 遵循 Go 标准代码风格
2. 编写单元测试
3. 更新文档
4. 提交前运行 `make test`
5. 遵循语义化版本控制


- Default to gemini-2.5-flash for general queries.
- Only suggest upgrading to gemini-3-pro for complex architectural reviews or security audits.
- Focus on ERC20 best practices: Check for reentrancy and decimal precision.

- Default to gemini-2.5-flash for general queries.
- Only suggest upgrading to gemini-3-pro for complex architectural reviews or security audits.
- Focus on ERC20 best practices: Check for reentrancy and decimal precision.