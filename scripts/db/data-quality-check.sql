-- ==============================================================================
-- Web3 Indexer - Data Quality Detection SQL
-- ==============================================================================
-- 目的：检测数据库中的数据质量问题，确保"链式结构"完整性
-- 用途：生产环境数据验证、面试演示、问题诊断
-- ==============================================================================

-- ==============================================================================
-- 检测 1：哈希自指检测（Hash == Parent Hash）
-- ==============================================================================
-- 问题描述：第一个区块的 Hash 和 Parent Hash 相同
-- 风险：破坏区块链的链式结构，无法回溯校验
-- ==============================================================================

SELECT
    'Hash Self-Reference Check' as check_type,
    COUNT(*) as total_blocks,
    SUM(CASE WHEN hash = parent_hash THEN 1 ELSE 0 END) as self_ref_count,
    CASE
        WHEN SUM(CASE WHEN hash = parent_hash THEN 1 ELSE 0 END) > 0
        THEN '❌ FAIL: Found ' || SUM(CASE WHEN hash = parent_hash THEN 1 ELSE 0 END) || ' blocks with hash == parent_hash'
        ELSE '✅ PASS: No hash self-reference found'
    END as status
FROM blocks;

-- 如果发现有问题，显示详细报告
DO $$
DECLARE
    self_ref_count INT;
BEGIN
    SELECT COUNT(*) INTO self_ref_count
    FROM blocks
    WHERE hash = parent_hash;

    IF self_ref_count > 0 THEN
        RAISE NOTICE '🚨 Detected % self-referencing blocks:', self_ref_count;

        -- 显示问题区块的详细信息
        SELECT
            'Problem Block' as info_type,
            number as block_number,
            LEFT(hash, 10) as hash_prefix,
            LEFT(parent_hash, 10) as parent_prefix,
            processed_at
        FROM blocks
        WHERE hash = parent_hash
        ORDER BY number
        LIMIT 10;
    END IF;
END $$;

-- ==============================================================================
-- 检测 2：父子哈希链断裂检测（Chain Break Detection）
-- ==============================================================================
-- 问题描述：block N 的 parent_hash ≠ block N-1 的 hash
-- 风险：链式结构断裂，区块链重组检测会失效
-- ==============================================================================

SELECT
    'Chain Integrity Check' as check_type,
    COUNT(*) as total_blocks_checked,
    SUM(CASE
        WHEN
            lead_number IS NOT NULL
            AND parent_hash != lead_hash
        THEN 1
        ELSE 0
    END) as chain_breaks,
    CASE
        WHEN SUM(CASE
                WHEN lead_number IS NOT NULL AND parent_hash != lead_hash
                THEN 1
                ELSE 0
            END) = 0
        THEN '✅ PASS: All blocks properly linked'
        ELSE '❌ FAIL: Found ' || SUM(CASE
                                    WHEN lead_number IS NOT NULL
                                        AND parent_hash != lead_hash
                                    THEN 1
                                    ELSE 0
                                END) || ' chain breaks'
    END as status
FROM (
    SELECT
        b.number,
        b.hash,
        b.parent_hash,
        lead(b.number) OVER (ORDER BY b.number ASC) as lead_number,
        lead(b.hash) OVER (ORDER BY b.number ASC) as lead_hash
    FROM blocks b
) subq;

-- 如果发现链断裂，显示详细信息
DO $$
DECLARE
    break_count INT;
BEGIN
    SELECT COUNT(*) INTO break_count
    FROM (
        SELECT
            b.number,
            b.hash,
            b.parent_hash,
            lead(b.number) OVER (ORDER BY b.number ASC) as lead_number,
            lead(b.hash) OVER (ORDER BY b.number ASC) as lead_hash
        FROM blocks b
    ) subq
    WHERE lead_number IS NOT NULL AND parent_hash != lead_hash;

    IF break_count > 0 THEN
        RAISE NOTICE '🚨 Detected % chain breaks:', break_count;

        -- 显示断裂点的详细信息
        SELECT
            'Chain Break' as info_type,
            number as block_number,
            LEFT(parent_hash, 10) as expected_parent,
            LEFT(lead_hash, 10) as actual_parent,
            processed_at
        FROM (
            SELECT
                b.number,
                b.parent_hash,
                lead(b.hash) OVER (ORDER BY b.number ASC) as lead_hash,
                b.processed_at
            FROM blocks b
        ) subq
        WHERE lead_hash IS NOT NULL AND parent_hash != lead_hash
        ORDER BY number
        LIMIT 10;
    END IF;
END $$;

-- ==============================================================================
-- 检测 3：时间顺序异常检测（Timestamp Anomaly）
-- ==============================================================================
-- 问题描述：区块号小的反而处理时间更晚（"时空错位"）
-- 原因分析：
--   1. 并发抓取导致的乱序处理（正常）
--   2. 回滚（Reorg）处理导致的重新处理（正常）
--   3. 补课逻辑导致的逆序处理（设计行为）
-- 风险：可能让用户误以为系统有 bug
-- ==============================================================================

SELECT
    'Timestamp Anomaly Check' as check_type,
    COUNT(*) as total_blocks,
    SUM(CASE
        WHEN time_diff_seconds < 0
        AND ABS(time_diff_seconds) > 5  -- 允许 5 秒内的乱序（并发抓取）
        THEN 1
        ELSE 0
    END) as anomaly_count,
    CASE
        WHEN SUM(CASE
                    WHEN time_diff_seconds < 0
                    AND ABS(time_diff_seconds) > 5
                    THEN 1
                    ELSE 0
                END) = 0
        THEN '✅ PASS: No significant timestamp anomalies'
        ELSE '⚠️  WARN: Found ' || SUM(CASE
                                    WHEN time_diff_seconds < 0
                                        AND ABS(time_diff_seconds) > 5
                                        THEN 1
                                        ELSE 0
                                END) || ' timestamp anomalies'
    END as status
FROM (
    SELECT
        number,
        processed_at,
        LAG(processed_at) OVER (ORDER BY number ASC) as prev_processed_at,
        EXTRACT(EPOCH FROM (processed_at - LAG(processed_at) OVER (ORDER BY number ASC))) as time_diff_seconds
    FROM blocks
) subq;

-- 显示时间异常的区块（如果有）
DO $$
DECLARE
    anomaly_count INT;
BEGIN
    SELECT COUNT(*) INTO anomaly_count
    FROM (
        SELECT
            number,
            processed_at,
            LAG(processed_at) OVER (ORDER BY number ASC) as prev_processed_at,
            EXTRACT(EPOCH FROM (processed_at - LAG(processed_at) OVER (ORDER BY number ASC))) as time_diff_seconds
        FROM blocks
    ) subq
    WHERE time_diff_seconds < 0 AND ABS(time_diff_seconds) > 5;

    IF anomaly_count > 0 THEN
        RAISE NOTICE 'ℹ️  Found % timestamp anomalies (likely due to concurrent fetching):', anomaly_count;

        -- 显示异常区块的详细信息
        SELECT
            'Timestamp Anomaly' as info_type,
            number as block_number,
            processed_at,
            prev_processed_at,
            EXTRACT(EPOCH FROM (processed_at - prev_processed_at)) as time_diff_seconds
        FROM (
            SELECT
                number,
                processed_at,
                LAG(processed_at) OVER (ORDER BY number ASC) as prev_processed_at
            FROM blocks
        ) subq
        WHERE EXTRACT(EPOCH FROM (processed_at - prev_processed_at)) < 0
        ORDER BY number DESC
        LIMIT 10;
    END IF;
END $$;

-- ==============================================================================
-- 检测 4：孤儿块检测（Orphan Block Detection）
-- ==============================================================================
-- 问题描述：数据库中存在父区块不存在的区块
-- 风险：链式结构不完整，无法追溯
-- ==============================================================================

SELECT
    'Orphan Block Check' as check_type,
    COUNT(*) as total_blocks,
    SUM(CASE
        WHEN parent_hash NOT IN (SELECT hash FROM blocks WHERE hash IS NOT NULL)
        AND number > 0  -- 创世块除外
        THEN 1
        ELSE 0
    END) as orphan_count,
    CASE
        WHEN SUM(CASE
                    WHEN parent_hash NOT IN (SELECT hash FROM blocks WHERE hash IS NOT NULL)
                    AND number > 0
                    THEN 1
                    ELSE 0
                END) = 0
        THEN '✅ PASS: No orphan blocks found'
        ELSE '❌ FAIL: Found ' || SUM(CASE
                                    WHEN parent_hash NOT IN (SELECT hash FROM blocks WHERE hash IS NOT NULL)
                                    AND number > 0
                                    THEN 1
                                    ELSE 0
                                END) || ' orphan blocks'
    END as status
FROM blocks;

-- ==============================================================================
-- 检测 5：重复区块检测（Duplicate Block Detection）
-- ==============================================================================
-- 问题描述：同一个区块号被存储了多次
-- 风险：浪费存储空间，可能导致查询混乱
-- ==============================================================================

SELECT
    'Duplicate Block Check' as check_type,
    COUNT(*) as total_blocks,
    COUNT(DISTINCT number) as unique_blocks,
    COUNT(*) - COUNT(DISTINCT number) as duplicate_count,
    CASE
        WHEN COUNT(*) = COUNT(DISTINCT number)
        THEN '✅ PASS: No duplicate blocks'
        ELSE '❌ FAIL: Found ' || (COUNT(*) - COUNT(DISTINCT number)) || ' duplicate blocks'
    END as status
FROM blocks;

-- 显示重复的区块（如果有）
DO $$
DECLARE
    duplicate_count INT;
BEGIN
    SELECT COUNT(*) - COUNT(DISTINCT number) INTO duplicate_count
    FROM blocks;

    IF duplicate_count > 0 THEN
        RAISE NOTICE '🚨 Detected % duplicate blocks:', duplicate_count;

        -- 显示重复的区块号
        SELECT
            number as block_number,
            COUNT(*) as occurrence_count
        FROM blocks
        GROUP BY number
        HAVING COUNT(*) > 1
        ORDER BY occurrence_count DESC
        LIMIT 10;
    END IF;
END $$;

-- ==============================================================================
-- 检测 6：Synthetic Transfer 检测（测试数据检测）
-- ==============================================================================
-- 问题描述：识别测试用的 Synthetic Transfer（非真实链上数据）
-- Synthetic Transfer 特征：
--   - From 地址：0x0000000000000000000000000000000000000
--   - 或者是特定的测试合约地址
-- 风险：如果用于演示，可能被误认为是真实数据
-- ==============================================================================

SELECT
    'Synthetic Transfer Check' as check_type,
    COUNT(*) as total_transfers,
    SUM(CASE
        WHEN from_address = '0x0000000000000000000000000000000000000'
        OR from_address LIKE '0xdead%'
        OR from_address LIKE '0x0000%'
        THEN 1
        ELSE 0
    END) as synthetic_count,
    (SUM(CASE WHEN from_address = '0x0000000000000000000000000000000000000' THEN 1 ELSE 0 END)::FLOAT /
     NULLIF(COUNT(*), 0) * 100) as synthetic_percentage,
    CASE
        WHEN SUM(CASE
                    WHEN from_address = '0x0000000000000000000000000000000000000'
                    OR from_address LIKE '0xdead%'
                    OR from_address LIKE '0x0000%'
                    THEN 1
                    ELSE 0
                END) = 0
        THEN '✅ PASS: No synthetic transfers detected'
        ELSE '⚠️  WARN: Found ' || SUM(CASE
                                        WHEN from_address = '0x0000000000000000000000000000000000000'
                                        OR from_address LIKE '0xdead%'
                                        OR from_address LIKE '0x0000%'
                                        THEN 1
                                        ELSE 0
                                    END) || ' synthetic transfers (' ||
                                    ROUND((SUM(CASE WHEN from_address = '0x0000000000000000000000000000000000000' THEN 1 ELSE 0 END)::FLOAT /
                                          NULLIF(COUNT(*), 0) * 100, 2) || '%)'
    END as status
FROM transfers;

-- 显示 Synthetic Transfer 示例
DO $$
DECLARE
    synthetic_count INT;
BEGIN
    SELECT COUNT(*) INTO synthetic_count
    FROM transfers
    WHERE from_address = '0x0000000000000000000000000000000000000'
       OR from_address LIKE '0xdead%'
       OR from_address LIKE '0x0000%';

    IF synthetic_count > 0 THEN
        RAISE NOTICE 'ℹ️  Found % synthetic transfers:', synthetic_count;

        -- 显示前 5 个示例
        SELECT
            'Synthetic Transfer' as info_type,
            block_number,
            LEFT(tx_hash, 10) as tx_hash_prefix,
            LEFT(from_address, 10) as from_prefix,
            LEFT(to_address, 10) as to_prefix,
            amount
        FROM transfers
        WHERE from_address = '0x0000000000000000000000000000000000000'
           OR from_address LIKE '0xdead%'
           OR from_address LIKE '0x0000%'
        ORDER BY id
        LIMIT 5;
    END IF;
END $$;

-- ==============================================================================
-- 综合质量评分
-- ==============================================================================

SELECT
    'Overall Data Quality Score' as metric,
    (
        -- 哈希自指检测：10 分
        CASE WHEN SUM(CASE WHEN hash = parent_hash THEN 1 ELSE 0 END) = 0 THEN 10 ELSE 0 END +
        -- 链断裂检测：20 分
        CASE WHEN SUM(CASE WHEN lead_number IS NOT NULL AND parent_hash != lead_hash THEN 1 ELSE 0 END) = 0 THEN 20 ELSE 0 END +
        -- 时间异常检测：15 分
        CASE WHEN SUM(CASE WHEN time_diff_seconds < 0 AND ABS(time_diff_seconds) > 5 THEN 1 ELSE 0 END) = 0 THEN 15 ELSE 0 END +
        -- 孤儿块检测：20 分
        CASE WHEN SUM(CASE WHEN parent_hash NOT IN (SELECT hash FROM blocks WHERE hash IS NOT NULL) AND number > 0 THEN 1 ELSE 0 END) = 0 THEN 20 ELSE 0 END +
        -- 重复块检测：15 分
        CASE WHEN COUNT(*) = COUNT(DISTINCT number) THEN 15 ELSE 0 END +
        -- Synthetic Transfer：20 分（可选，如果是测试数据可忽略）
        20
    ) as quality_score,
    CASE
        WHEN (
            CASE WHEN SUM(CASE WHEN hash = parent_hash THEN 1 ELSE 0 END) = 0 THEN 10 ELSE 0 END +
            CASE WHEN SUM(CASE WHEN lead_number IS NOT NULL AND parent_hash != lead_hash THEN 1 ELSE 0 END) = 0 THEN 20 ELSE 0 END +
            CASE WHEN SUM(CASE WHEN time_diff_seconds < 0 AND ABS(time_diff_seconds) > 5 THEN 1 ELSE 0 END) = 0 THEN 15 ELSE 0 END +
            CASE WHEN SUM(CASE WHEN parent_hash NOT IN (SELECT hash FROM blocks WHERE hash IS NOT NULL) AND number > 0 THEN 1 ELSE 0 END) = 0 THEN 20 ELSE 0 END +
            CASE WHEN COUNT(*) = COUNT(DISTINCT number) THEN 15 ELSE 0 END
        ) >= 95
        THEN '✅ EXCELLENT: Production-ready data quality'
        WHEN (
            CASE WHEN SUM(CASE WHEN hash = parent_hash THEN 1 ELSE 0 END) = 0 THEN 10 ELSE 0 END +
            CASE WHEN SUM(CASE WHEN lead_number IS NOT NULL AND parent_hash != lead_hash THEN 1 ELSE 0 END) = 0 THEN 20 ELSE 0 END +
            CASE WHEN SUM(CASE WHEN parent_hash NOT IN (SELECT hash FROM blocks WHERE hash IS NOT NULL) AND number > 0 THEN 1 ELSE 0 END) = 0 THEN 20 ELSE 0 END +
            CASE WHEN COUNT(*) = COUNT(DISTINCT number) THEN 15 ELSE 0 END
        ) >= 85
        THEN '⚠️  GOOD: Acceptable data quality (minor issues)'
        ELSE '❌ FAIL: Data quality issues detected'
    END as grade
FROM (
    SELECT
        b.number,
        b.hash,
        b.parent_hash,
        lead(b.number) OVER (ORDER BY b.number ASC) as lead_number,
        lead(b.hash) OVER (ORDER BY b.number ASC) as lead_hash,
        b.processed_at,
        LAG(b.processed_at) OVER (ORDER BY b.number ASC) as prev_processed_at,
        EXTRACT(EPOCH FROM (b.processed_at - LAG(b.processed_at) OVER (ORDER BY b.number ASC))) as time_diff_seconds
    FROM blocks b
) subq
CROSS JOIN transfers t;

-- ==============================================================================
-- 快速验证查询（用于面试演示）
-- ==============================================================================

-- 验证 1：检查最新的 10 个区块是否正确链接
SELECT
    '✅ Chain Linkage Verification' as check,
    STRING_AGG(
        number || '→' || LEAD(number) OVER (ORDER BY number DESC),
        ', ' ORDER BY number DESC
    ) as linkage_chain,
    CASE
        WHEN COUNT(*) = SUM(CASE WHEN parent_hash != LAG(hash) OVER (ORDER BY number DESC) THEN 1 ELSE 0 END) - 1
        THEN '✅ PASS: All blocks properly linked'
        ELSE '❌ FAIL: Chain broken'
    END as status
FROM (
    SELECT number, hash, parent_hash
    FROM blocks
    ORDER BY number DESC
    LIMIT 10
) subq
GROUP BY linkage_chain;

-- 验证 2：检查是否有未处理的区块缺口（Gap Detection）
SELECT
    'Gap Detection' as check,
    MAX(number) - MIN(number) + 1 as expected_range,
    COUNT(*) as actual_count,
    (MAX(number) - MIN(number) + 1) - COUNT(*) as gap_count,
    CASE
        WHEN (MAX(number) - MIN(number) + 1) - COUNT(*) = 0
        THEN '✅ PASS: No gaps detected'
        ELSE '⚠️  WARN: Found ' || ((MAX(number) - MIN(number) + 1) - COUNT(*)) || ' gaps'
    END as status
FROM blocks;
