#!/bin/bash

# 🚀 Web3 Indexer Demo - 一键启动全生态
# 实现合约地址自动化更新和热启动

set -e

echo "🌟 启动 Web3 Indexer Demo 全生态..."

# 1. 启动基础设施 (Anvil + DB)
echo "📦 启动基础设施..."
docker compose down 2>/dev/null || true
docker compose up -d anvil db

# 2. 等待 Anvil 启动
echo "⏳ 等待 Anvil 启动..."
sleep 5

# 3. 部署合约并抓取地址
echo "🚀 部署 ERC20 合约..."
source venv/bin/activate

# 部署合约并提取地址
DEPLOY_OUTPUT=$(python3 scripts/deploy_and_simulate.py 2>&1 | head -30)
CONTRACT_ADDRESS=$(echo "$DEPLOY_OUTPUT" | grep "Address:" | awk '{print $3}')

if [ -z "$CONTRACT_ADDRESS" ]; then
    echo "❌ 无法获取合约地址"
    exit 1
fi

echo "✅ 合约部署成功: $CONTRACT_ADDRESS"

# 4. 自动注入到 docker-compose.yml
echo "🔧 自动更新 docker-compose.yml..."
sed -i "s/WATCH_ADDRESSES=.*/WATCH_ADDRESSES: \"$CONTRACT_ADDRESS\"/" docker-compose.yml

# 5. 启动 Indexer
echo "🔄 启动 Indexer..."
docker compose up -d indexer

# 6. 等待 Indexer 配置完成
echo "⏳ 等待 Indexer 配置..."
sleep 10

# 7. 验证配置
CONFIGURED_ADDRESS=$(docker compose logs indexer | grep "watched_addresses_configured" | tail -1 | grep -o '"addresses":"[^"]*"' | cut -d'"' -f4)
echo "📋 Indexer 配置地址: $CONFIGURED_ADDRESS"

if [ "$CONTRACT_ADDRESS" != "$CONFIGURED_ADDRESS" ]; then
    echo "⚠️  地址不匹配: 合约=$CONTRACT_ADDRESS, Indexer=$CONFIGURED_ADDRESS"
fi

# 8. 启动背景流量生成
echo "🚀 启动流量生成..."
pkill -f deploy_and_simulate 2>/dev/null || true
nohup python3 -u scripts/deploy_and_simulate.py > simulation.log 2>&1 &
SIMULATION_PID=$!

echo "📊 仿真引擎 PID: $SIMULATION_PID"

# 9. 显示监控命令
echo ""
echo "🎯 系统已启动！使用以下命令监控："
echo ""
echo "📺 窗口 A - Indexer Transfer 事件处理:"
echo "   docker compose logs -f indexer | grep -E 'block_processed|✅|🎯|🔍'"
echo ""
echo "📺 窗口 B - 仿真引擎状态:"
echo "   tail -f simulation.log"
echo ""
echo "📺 窗口 C - 数据库计数:"
echo "   watch -n 5 \"docker exec web3-indexer-db psql -U postgres -d web3_indexer -c 'SELECT COUNT(*) as total_blocks FROM blocks; SELECT COUNT(*) as total_transfers FROM transfers;'\""
echo ""
echo "🌐 Dashboard: http://localhost:8080"
echo "📊 API Status: curl http://localhost:8080/api/status | jq ."
echo ""
echo "⏳ 等待 Indexer 追赶进度... (仿真在 block ~100+, Indexer 从 block 0 开始)"

# 10. 实时状态监控
echo ""
echo "📈 实时状态监控 (每10秒更新):"
while true; do
    STATUS=$(curl -s http://localhost:8080/api/status 2>/dev/null | jq -r '.latest_block + " | Blocks: " + (.total_blocks|tostring) + " | Transfers: " + (.total_transfers|to_string)' 2>/dev/null || echo "API 不可用")
    echo "[$(date '+%H:%M:%S')] $STATUS"
    sleep 10
done
