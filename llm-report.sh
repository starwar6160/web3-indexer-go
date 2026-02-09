#!/bin/bash
# 生成LLM分析报告 - 纯文本格式，方便LLM理解

echo "═══════════════════════════════════════════════════════════"
echo "  Web3 Indexer - LLM分析报告"
echo "  生成时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "═══════════════════════════════════════════════════════════"
echo ""

echo "【一、系统状态】"
echo "───────────────────────────────────────────────────────────"
echo "Docker容器状态:"
docker compose ps --format json 2>/dev/null | jq -r '.[] | "\(.Name): \(.State)"' 2>/dev/null || docker compose ps
echo ""

echo "端口监听状态:"
echo "  PostgreSQL (15432): $(lsof -i:15432 2>/dev/null | grep -c LISTEN || echo "0") 个连接"
echo "  Anvil RPC (8545): $(lsof -i:8545 2>/dev/null | grep -c LISTEN || echo "0") 个连接"
echo "  Indexer API (8080): $(lsof -i:8080 2>/dev/null | grep -c LISTEN || echo "0") 个连接"
echo ""

echo "【二、API健康检查】"
echo "───────────────────────────────────────────────────────────"
HEALTH=$(curl -s http://localhost:8080/healthz 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "$HEALTH" | jq -r '
        "整体状态: " + .status,
        "时间戳: " + .timestamp,
        "",
        "组件状态:",
        "  数据库: " + .checks.database.status + " (" + .checks.database.latency + ")",
        "  Fetcher: " + .checks.fetcher.status,
        "  RPC: " + .checks.rpc.status + " (" + .checks.rpc.message + ")",
        "  Sequencer: " + .checks.sequencer.status + " (" + .checks.sequencer.message + ")"
    ' 2>/dev/null || echo "$HEALTH"
else
    echo "❌ API无响应"
fi
echo ""

echo "【三、同步状态】"
echo "───────────────────────────────────────────────────────────"
STATUS=$(curl -s http://localhost:8080/api/status 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "$STATUS" | jq -r '
        "服务状态: " + .state,
        "最新区块: " + .latest_block,
        "同步延迟: " + (.sync_lag|tostring) + " blocks",
        "已处理区块: " + (.total_blocks|tostring),
        "交易总数: " + (.total_transfers|tostring),
        "健康状态: " + (if .is_healthy then "✅ 健康" else "❌ 不健康" end)
    ' 2>/dev/null || echo "$STATUS"
else
    echo "❌ 状态API无响应"
fi
echo ""

echo "【四、数据库状态】"
echo "───────────────────────────────────────────────────────────"
DB_INFO=$(docker exec web3-indexer-db psql -U postgres -d web3_indexer -t -c "
    SELECT '总区块数: ' || COUNT(*) FROM blocks
    UNION ALL
    SELECT '总交易数: ' || COUNT(*) FROM transfers
    UNION ALL
    SELECT '最新区块: ' || MAX(number) FROM blocks
    UNION ALL
    SELECT '最早区块: ' || MIN(number) FROM blocks;
" 2>/dev/null | sed 's/^[ \t]*//')

if [ -n "$DB_INFO" ]; then
    echo "$DB_INFO"
else
    echo "❌ 无法连接数据库"
fi
echo ""

echo "【五、最新区块记录（最新5条）】"
echo "───────────────────────────────────────────────────────────"
docker exec web3-indexer-db psql -U postgres -d web3_indexer -t -c "
    SELECT
        '区块#' || number as 区块,
        '哈希: ' || substring(hash, 1, 16) || '...' as hash,
        '时间: ' || timestamp as ts,
        '处理: ' || processed_at::text as processed
    FROM blocks
    ORDER BY number DESC
    LIMIT 5;
" 2>/dev/null | sed 's/^[ \t]*//' | head -10
echo ""

echo "【六、数据一致性检查】"
echo "───────────────────────────────────────────────────────────"
CONSISTENCY=$(docker exec web3-indexer-db psql -U postgres -d web3_indexer -t -c "
    WITH numbered_blocks AS (
      SELECT number, LEAD(number) OVER (ORDER BY number) as next_number
      FROM blocks
    )
    SELECT
        '区块连续性: ' || CASE
            WHEN COUNT(*) FILTER (WHERE next_number IS NOT NULL AND next_number != number + 1) = 0
            THEN '✅ 通过 (无gaps)'
            ELSE '❌ 失败 (存在gaps)'
        END as result
    FROM numbered_blocks;
" 2>/dev/null | sed 's/^[ \t]*//')

if [ -n "$CONSISTENCY" ]; then
    echo "$CONSISTENCY"
else
    echo "❌ 无法检查一致性"
fi
echo ""

echo "【七、最近的关键日志（最后30条）】"
echo "───────────────────────────────────────────────────────────"
tail -300 /tmp/indexer.log 2>/dev/null | grep -v "^$" | tail -30 || echo "无日志"
echo ""

echo "【八、错误和警告（如有）】"
echo "───────────────────────────────────────────────────────────"
ERRORS=$(tail -500 /tmp/indexer.log 2>/dev/null | grep -iE "error|warn|panic" | tail -10)
if [ -n "$ERRORS" ]; then
    echo "$ERRORS"
else
    echo "✅ 无错误或警告"
fi
echo ""

echo "【九、性能指标】"
echo "───────────────────────────────────────────────────────────"
# 计算最近的处理速度
tail -100 /tmp/indexer.log 2>/dev/null | grep "block_processed" | tail -10 | while IFS= read -r line; do
    echo "$line"
done
echo ""

echo "【十、LLM判断建议】"
echo "───────────────────────────────────────────────────────────"
echo "请根据以上信息判断:"
echo "1. 系统是否正常运行？"
echo "2. 是否有错误或异常需要处理？"
echo "3. 数据一致性是否良好？"
echo "4. 同步进度是否符合预期？"
echo "5. 是否有性能瓶颈？"
echo "═══════════════════════════════════════════════════════════"
