#!/bin/bash
# 查看数据库最新记录 - 专为LLM分析优化

echo "=================================="
echo "💾 数据库概览"
echo "=================================="
docker exec web3-indexer-db psql -U postgres -d web3_indexer -t -c "
SELECT '总区块数: ' || COUNT(*) FROM blocks
UNION ALL
SELECT '总交易数: ' || COUNT(*) FROM transfers
UNION ALL
SELECT '最新区块号: ' || MAX(number) FROM blocks
UNION ALL
SELECT '最早区块号: ' || MIN(number) FROM blocks;
" | sed 's/^[ \t]*//'

echo ""
echo "=================================="
echo "📦 最新5条区块记录"
echo "=================================="
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
SELECT
    number as 区块号,
    substring(hash, 1, 16) || '...' as 区块哈希,
    substring(parent_hash, 1, 16) || '...' as 父哈希,
    timestamp as 时间戳,
    processed_at as 处理时间
FROM blocks
ORDER BY number DESC
LIMIT 5;
"

echo ""
echo "=================================="
echo "💸 最新5条转账记录（如有）"
echo "=================================="
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
SELECT
    block_number as 区块号,
    substring(tx_hash, 1, 16) || '...' as 交易哈希,
    log_index as 日志索引,
    substring(from_address, 1, 10) || '...' as 发送方,
    substring(to_address, 1, 10) || '...' as 接收方,
    amount as 金额
FROM transfers
ORDER BY block_number DESC, log_index DESC
LIMIT 5;
" || echo "暂无转账记录"

echo ""
echo "=================================="
echo "✅ 数据一致性检查"
echo "=================================="
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
WITH numbered_blocks AS (
  SELECT number, LEAD(number) OVER (ORDER BY number) as next_number
  FROM blocks
)
SELECT
    '区块连续性' as 检查项,
    CASE
        WHEN COUNT(*) FILTER (WHERE next_number IS NOT NULL AND next_number != number + 1) = 0
        THEN '✅ 通过 (无gaps)'
        ELSE '❌ 失败 (存在gaps: ' || COUNT(*) FILTER (WHERE next_number IS NOT NULL AND next_number != number + 1) || ')'
    END as 结果
FROM numbered_blocks
UNION ALL
SELECT
    '父哈希链完整性',
    CASE
        WHEN (
            SELECT COUNT(*)
            FROM (
                SELECT number, parent_hash, LAG(hash) OVER (ORDER BY number) as prev_hash
                FROM blocks
            ) t
            WHERE number > 0 AND parent_hash != prev_hash
        ) = 0
        THEN '✅ 通过 (无断裂)'
        ELSE '❌ 失败 (链断裂)'
    END;
"

echo ""
echo "=================================="
echo "🔄 同步检查点状态"
echo "=================================="
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
SELECT
    chain_id as 链ID,
    last_synced_block as 最后同步区块,
    updated_at as 更新时间
FROM sync_checkpoints
ORDER BY chain_id;
"
