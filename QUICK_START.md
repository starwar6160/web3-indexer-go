# 快速开始指南 - Web3 Indexer 快速开发循环

## 🚀 5分钟快速启动

### 1. 启动基础设施（首次运行）

```bash
# 启动PostgreSQL和Anvil
docker compose up -d db anvil

# 等待容器健康（约5秒）
sleep 5

# 验证容器状态
docker compose ps
```

**预期输出**:
```
NAME                 STATUS                    PORTS
web3-indexer-anvil   Up 5 seconds (healthy)    0.0.0.0:8545->8545/tcp
web3-indexer-db      Up 5 seconds (healthy)    0.0.0.0:15432->5432/tcp
```

### 2. 初始化数据库（仅首次运行）

```bash
# 运行数据库迁移
docker exec -i web3-indexer-db psql -U postgres -d web3_indexer < migrations/001_init.sql

# 验证表已创建
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "\dt"
```

### 3. 启动Indexer

```bash
# 使用快速启动脚本
./dev-run.sh

# 或者手动运行（更多控制）
export DATABASE_URL="postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable"
export RPC_URLS="http://localhost:8545"
export CHAIN_ID="31337"
export START_BLOCK="0"
export API_PORT="8080"
go run cmd/indexer/main.go
```

**预期输出**:
```
🚀 启动Web3 Indexer（快速开发循环模式）
================================================
📡 配置信息：
  - 数据库: localhost:15432
  - RPC: http://localhost:8545
  - API端口: 8080 (监听 0.0.0.0)
  - Chain ID: 31337

✅ 启动Indexer...
{"time":"...","level":"INFO","msg":"starting_web3_indexer",...}
{"time":"...","level":"INFO","msg":"sequencer_started",...}
```

### 4. 验证运行状态

```bash
# 健康检查
curl -s http://localhost:8080/healthz | jq '.'

# 查看同步状态
curl -s http://localhost:8080/api/status | jq '.'

# 查看最新区块
curl -s http://localhost:8080/api/blocks | jq '.blocks[0:3]'
```

### 5. 访问Dashboard

在浏览器中打开：
```
http://localhost:8080/
```

或从其他机器访问（替换为您的Ubuntu IP）：
```
http://192.168.0.8:8080/
```

---

## 🔄 快速开发循环

### 典型工作流程

```bash
# 1. 修改代码
vim internal/engine/processor.go

# 2. 停止Indexer（Ctrl+C）
# 按 Ctrl+C 停止当前运行的Indexer

# 3. 重新运行（无需编译，立即生效）
./dev-run.sh
```

**优势**:
- ✅ 无需编译，修改立即生效
- ✅ 快速迭代，节省时间
- ✅ 容器基础设施保持运行
- ✅ 数据持久化，无需重新同步

---

## 🛠️ 常用命令

### 基础设施管理

```bash
# 启动基础设施
docker compose up -d db anvil

# 查看日志
docker compose logs -f db
docker compose logs -f anvil

# 停止基础设施
docker compose down

# 完全清理（包括数据）
docker compose down -v
```

### Indexer管理

```bash
# 启动Indexer
./dev-run.sh

# 后台运行
nohup ./dev-run.sh > /tmp/indexer.log 2>&1 &

# 查看日志
tail -f /tmp/indexer.log

# 停止Indexer
pkill -f "go run cmd/indexer"
# 或直接按 Ctrl+C
```

### 数据库查询

```bash
# 检查区块数量
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c \
  "SELECT COUNT(*) FROM blocks;"

# 查看最新区块
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c \
  "SELECT number, hash FROM blocks ORDER BY number DESC LIMIT 5;"

# 查看同步状态
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c \
  "SELECT * FROM sync_status;"
```

### 验证和测试

```bash
# 验证区块连续性
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
WITH numbered_blocks AS (
  SELECT number, LEAD(number) OVER (ORDER BY number) as next_number
  FROM blocks
)
SELECT COUNT(*) as gaps FROM numbered_blocks
WHERE next_number IS NOT NULL AND next_number != number + 1;
"

# 验证父哈希链
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
WITH block_chain AS (
  SELECT number, hash, parent_hash, LAG(hash) OVER (ORDER BY number) as prev_hash
  FROM blocks
)
SELECT COUNT(*) as chain_breaks FROM block_chain
WHERE number > 0 AND parent_hash != prev_hash;
"

# 测试RPC连接
curl -s http://localhost:8545 -X POST \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

---

## 🔧 故障排查

### 容器未启动

```bash
# 检查容器状态
docker compose ps

# 查看容器日志
docker compose logs db
docker compose logs anvil

# 重启容器
docker compose restart db anvil
```

### 端口冲突

```bash
# 检查端口占用
lsof -i:15432  # PostgreSQL
lsof -i:8545   # Anvil
lsof -i:8080   # Indexer

# 清理端口（如需要）
sudo lsof -ti:8080 | xargs kill -9
```

### 数据库连接失败

```bash
# 测试数据库连接
docker exec web3-indexer-db pg_isready -U postgres

# 检查数据库
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "SELECT 1;"
```

### Indexer无法启动

```bash
# 检查环境变量
echo $DATABASE_URL
echo $RPC_URLS
echo $CHAIN_ID

# 查看详细日志
tail -50 /tmp/indexer.log

# 查找错误
grep -i error /tmp/indexer.log
```

### 外部无法访问

```bash
# 检查防火墙
sudo ufw status
sudo ufw allow 8080/tcp

# 检查监听地址
lsof -i:8080
# 应显示: *:http-alt (LISTEN)

# 获取本机IP
hostname -I
```

---

## 📊 监控和调试

### 实时监控

```bash
# 监控Indexer日志
tail -f /tmp/indexer.log | grep "block_processed"

# 监控Anvil出块
docker compose logs -f anvil | grep "Block Number"

# 监控数据库查询
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c \
  "SELECT COUNT(*) FROM blocks;" | watch -n 1
```

### 性能指标

```bash
# 查看Prometheus指标
curl -s http://localhost:8080/metrics | grep indexer

# 查看处理速度
curl -s http://localhost:8080/api/status | jq '{total_blocks, sync_lag}'
```

---

## 🎯 下一步

1. **监控Dashboard**: 访问 http://localhost:8080
2. **API文档**: 查看所有可用端点
3. **修改代码**: 开始快速迭代开发
4. **运行测试**: `go test ./...`
5. **集成仿真器**: 配置emulator生成测试交易

---

## 📖 更多资源

- [完整验证报告](VERIFICATION_REPORT.md)
- [CLAUDE.md](CLAUDE.md) - 项目架构和设计
- [docker-compose.yml](docker-compose.yml) - 基础设施配置
- [migrations/001_init.sql](migrations/001_init.sql) - 数据库schema

---

**祝您开发愉快！** 🎉
