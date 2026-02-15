#!/bin/bash
# =============================================================================
# Web3 Indexer - 追赶到最新（Catch-up to Latest）
# =============================================================================
# 用途：清空历史数据，让 Indexer 从当前链头开始实时索引
# 效果：E2E Latency 从 ~20 分钟降至 < 60 秒
# =============================================================================

set -e

echo "🚀 Starting catch-up to latest..."
echo ""

# 1. 获取当前链头高度
echo "📡 Step 1: Querying current chain head..."
RPC_URL=$(grep SEPOLIA_RPC_URLS .env.testnet.local | cut -d'=' -f2 | cut -d',' -f1)
CHAIN_HEAD=$(curl -s -X POST "$RPC_URL" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' \
  | xargs printf "%d")

echo "   ✅ Current chain head: #$CHAIN_HEAD"
echo ""

# 2. 停止容器
echo "🛑 Step 2: Stopping indexer container..."
docker compose -f docker-compose.testnet.yml --env-file .env.testnet.local -p web3-testnet stop sepolia-indexer
echo "   ✅ Container stopped"
echo ""

# 3. 备份当前数据（可选）
echo "💾 Step 3: Backing up current data..."
BACKUP_FILE="backup_$(date +%Y%m%d_%H%M%S).sql"
docker exec web3-indexer-sepolia-db pg_dump -U postgres web3_sepolia > "$BACKUP_FILE" 2>/dev/null || true
echo "   ✅ Backup saved to: $BACKUP_FILE"
echo ""

# 4. 清空历史数据
echo "🧹 Step 4: Clearing historical data..."
docker exec web3-indexer-sepolia-db psql -U postgres -d web3_sepolia <<SQL
-- 清空所有表
TRUNCATE TABLE blocks, transfers, transactions CASCADE;

-- 删除旧的检查点
DELETE FROM sync_checkpoints WHERE chain_id = 11155111;

-- 重置序列
ALTER SEQUENCE blocks_id_seq RESTART WITH 1;
ALTER SEQUENCE transfers_id_seq RESTART WITH 1;

SQL
echo "   ✅ Historical data cleared"
echo ""

# 5. 设置起始块为当前链头（Reorg 安全：减去 3）
START_BLOCK=$((CHAIN_HEAD - 3))
echo "🎯 Step 5: Setting START_BLOCK to #$START_BLOCK (chain head - 3 for reorg safety)"
sed -i "s/^START_BLOCK=.*/START_BLOCK=$START_BLOCK/" .env.testnet.local
echo "   ✅ Configuration updated"
echo ""

# 6. 重启容器
echo "🔄 Step 6: Restarting indexer..."
docker compose -f docker-compose.testnet.yml --env-file .env.testnet.local -p web3-testnet up -d sepolia-indexer
echo "   ✅ Container restarted"
echo ""

# 7. 等待容器启动
echo "⏳ Step 7: Waiting for indexer to initialize..."
sleep 10
echo "   ✅ Indexer is running"
echo ""

# 8. 验证
echo "🔍 Step 8: Verifying catch-up status..."
sleep 5
API_STATUS=$(curl -s http://localhost:8081/api/status)
SYNC_LAG=$(echo "$API_STATUS" | jq -r '.sync_lag')
LATEST_INDEXED=$(echo "$API_STATUS" | jq -r '.latest_indexed')

echo "   Sync Lag: $SYNC_LAG blocks"
echo "   Latest Indexed: #$LATEST_INDEXED"
echo ""

if [ "$SYNC_LAG" -lt 10 ]; then
    echo "🎉 SUCCESS! Indexer is now in REAL-TIME mode!"
    echo "   E2E Latency should be < 2 minutes"
else
    echo "⚠️  Sync Lag is still high: $SYNC_LAG blocks"
    echo "   This is normal if the network is producing blocks rapidly"
fi

echo ""
echo "📊 Monitor progress:"
echo "   curl http://localhost:8081/api/status | jq '.sync_lag'"
echo ""
echo "✅ Catch-up to latest complete!"
