-- Web3 Indexer 数据完整性审计脚本
-- 目标：6 个 9 的持久性与一致性验证

-- 1. 区块连续性检测 (Gap Detection)
-- 使用窗口函数检查是否存在跳过的区块序号
SELECT 
    prev_nr + 1 AS gap_start,
    next_nr - 1 AS gap_end,
    (next_nr - prev_nr - 1) AS gap_size
FROM (
    SELECT 
        number AS prev_nr, 
        LEAD(number) OVER (ORDER BY number) AS next_nr
    FROM blocks
) s
WHERE next_nr - prev_nr > 1;

-- 2. 地址提取完整性检测
-- 识别那些由于早期逻辑 Bug 产生的 0xunknown 发送者
SELECT 
    block_number, 
    tx_hash,
    processed_at
FROM transfers 
WHERE from_address = '0xunknown'
ORDER BY block_number DESC;

-- 3. 数据总量快照对比
SELECT 
    (SELECT COUNT(*) FROM blocks) as total_blocks,
    (SELECT COUNT(*) FROM transfers) as total_transfers,
    (SELECT MAX(number) FROM blocks) as max_block_height;
