#!/bin/bash
# 一键检查系统状态 - 专为LLM分析优化

echo "╔════════════════════════════════════════════════════════════╗"
echo "║         Web3 Indexer 系统状态报告                         ║"
echo "║         生成时间: $(date '+%Y-%m-%d %H:%M:%S')              ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""

echo "🐳 Docker容器状态"
echo "────────────────────────────────────────"
docker compose ps
echo ""

echo "🔌 端口监听状态"
echo "────────────────────────────────────────"
echo "PostgreSQL (15432):"
lsof -i:15432 | head -2 || echo "  ❌ 未监听"
echo ""
echo "Anvil RPC (8545):"
lsof -i:8545 | head -2 || echo "  ❌ 未监听"
echo ""
echo "Indexer API (8080):"
lsof -i:8080 | head -2 || echo "  ❌ 未监听"
echo ""

echo "📡 API健康检查"
echo "────────────────────────────────────────"
HEALTH=$(curl -s http://localhost:8080/healthz 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "$HEALTH" | jq '.' 2>/dev/null || echo "$HEALTH"
else
    echo "❌ API无响应"
fi
echo ""

echo "📊 同步状态"
echo "────────────────────────────────────────"
STATUS=$(curl -s http://localhost:8080/api/status 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "$STATUS" | jq '.' 2>/dev/null || echo "$STATUS"
else
    echo "❌ 状态API无响应"
fi
echo ""

echo "🔍 最近的关键事件（最后20条）"
echo "────────────────────────────────────────"
tail -200 /tmp/indexer.log 2>/dev/null | grep -v "^$" | tail -20 || echo "无日志"
echo ""

echo "⚠️  错误和警告（如有）"
echo "────────────────────────────────────────"
ERRORS=$(tail -500 /tmp/indexer.log 2>/dev/null | grep -iE "error|warn|panic" | tail -5)
if [ -n "$ERRORS" ]; then
    echo "$ERRORS"
else
    echo "✅ 无错误或警告"
fi
echo ""

echo "💾 数据库快照"
echo "────────────────────────────────────────"
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
SELECT
    '区块总数' as 指标, COUNT(*) as 值 FROM blocks
UNION ALL
SELECT '交易总数', COUNT(*) FROM transfers
UNION ALL
SELECT '最新区块', MAX(number)::text FROM blocks
UNION ALL
SELECT '区块范围',
    (SELECT MIN(number)::text FROM blocks) || ' -> ' || (SELECT MAX(number)::text FROM blocks)
UNION ALL
SELECT '连续性检查',
    CASE
        WHEN (SELECT COUNT(*) FROM blocks WHERE number > 0 AND parent_hash NOT IN (SELECT hash FROM blocks WHERE number = blocks.number - 1)) = 0
        THEN '✅ 连续'
        ELSE '❌ 存在gaps'
    END;
" 2>/dev/null || echo "无法连接数据库"
echo ""

echo "╔════════════════════════════════════════════════════════════╗"
echo "║  报告结束                                                   ║"
echo "╚════════════════════════════════════════════════════════════╝"
