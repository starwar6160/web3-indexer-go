# 🚀 Web3 Indexer 端到端验证命令清单

## 第一步：启动完整系统（3个终端窗口）

### 窗口1：启动Indexer（持续运行模式）
```bash
cd /home/ubuntu/zwCode/web3indexergo/web3-indexer-go

# 启动Indexer，监听端口2090
CONTINUOUS_MODE=true \
RPC_URLS=http://localhost:8545 \
DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable \
CHAIN_ID=31337 \
START_BLOCK=0 \
WATCH_ADDRESSES=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707 \
API_PORT=2090 \
LOG_LEVEL=info \
LOG_FORMAT=json \
go run cmd/indexer/main.go
```

### 窗口2：实时监控Indexer日志（块处理进度）
```bash
cd /home/ubuntu/zwCode/web3indexergo/web3-indexer-go

# 监控块处理日志
timeout 120 bash -c 'CONTINUOUS_MODE=true \
RPC_URLS=http://localhost:8545 \
DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable \
CHAIN_ID=31337 \
START_BLOCK=0 \
WATCH_ADDRESSES=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707 \
API_PORT=2090 \
LOG_LEVEL=info \
LOG_FORMAT=json \
go run cmd/indexer/main.go' 2>&1 | grep -E "block_processed|Sequencer received|Transfer|Schedule"
```

### 窗口3：实时查询数据库中的Transfer事件
```bash
# 每5秒查询一次数据库中的Transfer事件数量
watch -n 5 'psql -h localhost -U postgres -d web3_indexer -c "SELECT COUNT(*) as transfer_count FROM transfers;" 2>/dev/null || echo "等待数据库连接..."'
```

---

## 第二步：验证API端点（在另一个终端）

### 2.1 检查Indexer健康状态
```bash
curl -s http://localhost:2090/api/status | jq '.'
```

**预期输出：**
```json
{
  "status": "active",
  "latest_block": 750,
  "synced_block": 750,
  "transfers_count": 150,
  "mode": "continuous"
}
```

### 2.2 查询Transfer事件
```bash
# 获取前10条Transfer事件
curl -s http://localhost:2090/api/transfers?limit=10 | jq '.transfers[0:5]'
```

**预期输出：**
```json
{
  "transfers": [
    {
      "from": "0x...",
      "to": "0x...",
      "value": "1000000000000000000",
      "block_number": 100,
      "transaction_hash": "0x..."
    }
  ]
}
```

### 2.3 查询特定合约的Transfer事件
```bash
# 查询监听地址的所有Transfer事件
curl -s "http://localhost:2090/api/transfers?contract=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707&limit=20" | jq '.transfers | length'
```

### 2.4 检查数据库同步进度
```bash
# 查询最新处理的块号
psql -h localhost -U postgres -d web3_indexer -c "SELECT MAX(block_number) as latest_block FROM blocks;"

# 查询Transfer事件总数
psql -h localhost -U postgres -d web3_indexer -c "SELECT COUNT(*) as transfer_count FROM transfers;"

# 查询特定合约的Transfer事件
psql -h localhost -U postgres -d web3_indexer -c "SELECT COUNT(*) FROM transfers WHERE contract_address = '0x5FC8d32690cc91D4c39d9d3abcBD16989F875707';"
```

---

## 第三步：性能监控

### 3.1 监控Indexer处理速度
```bash
# 每10秒输出一次处理速度（块/秒）
watch -n 10 'echo "=== Indexer Performance ===" && \
psql -h localhost -U postgres -d web3_indexer -c "SELECT MAX(block_number) FROM blocks;" && \
echo "Transfers:" && \
psql -h localhost -U postgres -d web3_indexer -c "SELECT COUNT(*) FROM transfers;"'
```

### 3.2 监控数据库连接
```bash
# 检查PostgreSQL连接状态
psql -h localhost -U postgres -d web3_indexer -c "SELECT datname, count(*) FROM pg_stat_activity GROUP BY datname;"
```

### 3.3 监控系统资源
```bash
# 监控Go进程的CPU和内存使用
watch -n 5 'ps aux | grep "go run cmd/indexer" | grep -v grep'
```

---

## 第四步：完整的端到端验证脚本

### 一键验证脚本
```bash
#!/bin/bash

echo "🚀 启动Web3 Indexer端到端验证..."
echo ""

# 1. 检查基础设施
echo "1️⃣ 检查基础设施..."
echo "   - Anvil: $(curl -s http://localhost:8545 -X POST -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result' 2>/dev/null || echo '❌ 不可用')"
echo "   - PostgreSQL: $(psql -h localhost -U postgres -d web3_indexer -c 'SELECT 1' 2>/dev/null && echo '✅ 可用' || echo '❌ 不可用')"
echo ""

# 2. 启动Indexer
echo "2️⃣ 启动Indexer..."
cd /home/ubuntu/zwCode/web3indexergo/web3-indexer-go
nohup bash -c 'CONTINUOUS_MODE=true \
RPC_URLS=http://localhost:8545 \
DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable \
CHAIN_ID=31337 \
START_BLOCK=0 \
WATCH_ADDRESSES=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707 \
API_PORT=2090 \
LOG_LEVEL=info \
LOG_FORMAT=json \
go run cmd/indexer/main.go' > indexer.log 2>&1 &

sleep 5

# 3. 检查Indexer状态
echo "3️⃣ 检查Indexer状态..."
curl -s http://localhost:2090/api/status | jq '.' 2>/dev/null || echo "❌ API不可用"
echo ""

# 4. 等待数据同步
echo "4️⃣ 等待数据同步（30秒）..."
sleep 30

# 5. 检查Transfer事件
echo "5️⃣ 检查Transfer事件..."
TRANSFER_COUNT=$(psql -h localhost -U postgres -d web3_indexer -c "SELECT COUNT(*) FROM transfers;" 2>/dev/null | tail -1 | tr -d ' ')
echo "   - Transfer事件总数: $TRANSFER_COUNT"
echo ""

# 6. 检查块处理进度
echo "6️⃣ 检查块处理进度..."
LATEST_BLOCK=$(psql -h localhost -U postgres -d web3_indexer -c "SELECT MAX(block_number) FROM blocks;" 2>/dev/null | tail -1 | tr -d ' ')
echo "   - 最新处理块: $LATEST_BLOCK"
echo ""

# 7. 验证API端点
echo "7️⃣ 验证API端点..."
echo "   - /api/status: $(curl -s http://localhost:2090/api/status | jq -r '.status' 2>/dev/null || echo '❌')"
echo "   - /api/transfers: $(curl -s http://localhost:2090/api/transfers?limit=1 | jq -r '.transfers | length' 2>/dev/null || echo '❌') Transfer事件"
echo ""

echo "✅ 验证完成！"
```

---

## 第五步：故障排查命令

### 如果Indexer无法启动
```bash
# 检查端口是否被占用
lsof -i :2090

# 杀死占用端口的进程
kill -9 <PID>

# 检查RPC连接
curl -s http://localhost:8545 -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq '.'

# 检查数据库连接
psql -h localhost -U postgres -d web3_indexer -c "SELECT 1;"
```

### 如果没有Transfer事件
```bash
# 检查是否有任何块被处理
psql -h localhost -U postgres -d web3_indexer -c "SELECT COUNT(*) FROM blocks;"

# 检查监听的合约地址
psql -h localhost -U postgres -d web3_indexer -c "SELECT DISTINCT contract_address FROM transfers LIMIT 5;"

# 检查仿真脚本是否在运行
ps aux | grep deploy_and_simulate

# 检查Indexer日志中的错误
tail -100 indexer.log | grep -i error
```

### 如果处理速度很慢
```bash
# 检查RPC节点健康状态
curl -s http://localhost:8545 -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"eth_syncing","params":[],"id":1}' | jq '.'

# 检查数据库性能
psql -h localhost -U postgres -d web3_indexer -c "SELECT * FROM pg_stat_statements ORDER BY mean_time DESC LIMIT 5;"

# 检查Indexer的并发配置
grep -i "concurrency\|workers" indexer.log
```

---

## 验证成功标志

✅ **系统正常运行的标志：**
1. Indexer启动时显示 `🚀 持续运行模式已开启，智能休眠已禁用`
2. 日志中持续出现 `block_processed` 和 `Sequencer received block`
3. API `/api/status` 返回 `"status": "active"`
4. 数据库中Transfer事件数量持续增加
5. 处理速度 > 10 blocks/second

❌ **故障标志：**
1. `port_conflict` - 端口被占用
2. `schedule_failed` - 任务调度失败
3. `database_connected` 后没有 `block_processed` - 引擎未启动
4. Transfer事件数量不增加 - 事件未被捕获
5. API无响应 - HTTP服务器未启动

