#!/bin/bash
# 验证代币过滤功能是否正常工作

set -e

CONTAINER_NAME="web3-debug-app"
DB_CONTAINER="web3-testnet-db"

echo "🔍 验证代币过滤功能..."
echo ""

# 1. 检查容器是否运行
if ! docker ps --format "{{.Names}}" | grep -q "^${CONTAINER_NAME}$"; then
    echo "❌ 容器 ${CONTAINER_NAME} 未运行"
    echo "   请先启动: docker-compose -f docker-compose.debug.yml up -d"
    exit 1
fi

echo "✅ 容器 ${CONTAINER_NAME} 正在运行"
echo ""

# 2. 检查代币过滤日志
echo "📋 检查代币过滤启用日志..."
if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Token filtering enabled"; then
    echo "✅ 代币过滤已启用"
    docker logs ${CONTAINER_NAME} 2>&1 | grep "Token filtering enabled" | tail -1
else
    echo "⚠️  未找到代币过滤日志"
fi
echo ""

# 3. 检查 RPC 节点健康状态
echo "🌐 检查 RPC 节点健康状态..."
HEALTHY_NODES=$(docker logs ${CONTAINER_NAME} 2>&1 | grep -oP "Enhanced RPC Pool initialized with \K[0-9]+" | tail -1)
if [ -n "$HEALTHY_NODES" ]; then
    echo "✅ 健康节点数: ${HEALTHY_NODES}/2"
else
    echo "⚠️  未找到 RPC 健康检查日志"
fi
echo ""

# 4. 检查监控的代币地址
echo "🎯 检查监控的代币地址..."
TOKEN_COUNT=$(docker logs ${CONTAINER_NAME} 2>&1 | grep -oP "watched_count.\K[0-9]+" | tail -1)
if [ -n "$TOKEN_COUNT" ]; then
    echo "✅ 监控代币数: ${TOKEN_COUNT}"
else
    echo "⚠️  未找到代币数量日志"
fi

# 显示代币地址
docker logs ${CONTAINER_NAME} 2>&1 | grep -A 1 "watched_count" | grep "0x" | tail -4 | while read addr; do
    if [ "$addr" != "" ]; then
        echo "   - $addr"
    fi
done
echo ""

# 5. 检查数据库中的代币数量
echo "💾 检查数据库中的代币数量..."
DISTINCT_TOKENS=$(docker exec ${DB_CONTAINER} psql -U postgres -d web3_sepolia -t -c "SELECT COUNT(DISTINCT token_address) FROM transfers;" 2>/dev/null | xargs)
if [ -n "$DISTINCT_TOKENS" ] && [ "$DISTINCT_TOKENS" != "" ]; then
    echo "✅ 数据库中不同的代币数量: ${DISTINCT_TOKENS}"
    if [ "$DISTINCT_TOKENS" -le 4 ]; then
        echo "   ✅ 符合预期（≤ 4 个热门代币）"
    else
        echo "   ⚠️  超出预期（应该 ≤ 4 个）"
    fi
else
    echo "⚠️  数据库查询失败或无数据"
fi
echo ""

# 6. 显示每个代币的转账数量
echo "📊 各代币转账统计..."
docker exec ${DB_CONTAINER} psql -U postgres -d web3_sepolia -c "
SELECT
    SUBSTRING(token_address, 1, 42) as token,
    COUNT(*) as transfers,
    MAX(block_number) as latest_block
FROM transfers
GROUP BY token_address
ORDER BY transfers DESC
LIMIT 10;" 2>/dev/null || echo "⚠️  查询失败"
echo ""

# 7. 检查最近的转账记录
echo "🕒 最近的转账记录（前 5 条）..."
docker exec ${DB_CONTAINER} psql -U postgres -d web3_sepolia -c "
SELECT
    SUBSTRING(token_address, 1, 10) as token,
    block_number,
    SUBSTRING(from_addr, 1, 10) as from_addr,
    SUBSTRING(to_addr, 1, 10) as to_addr,
    amount_raw
FROM transfers
ORDER BY block_number DESC
LIMIT 5;" 2>/dev/null || echo "⚠️  查询失败"
echo ""

echo "✅ 验证完成！"
echo ""
echo "📝 预期结果："
echo "   - 代币过滤已启用"
echo "   - 监控 4 个代币（USDC, DAI, WETH, UNI）"
echo "   - 数据库中 ≤ 4 个不同的代币地址"
echo "   - 只显示热门代币的转账记录"
