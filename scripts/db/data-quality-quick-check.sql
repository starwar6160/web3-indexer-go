-- ==============================================================================
-- Web3 Indexer - 简化数据质量检测 SQL
-- ==============================================================================
-- 用途：快速验证数据库完整性，适合面试演示

-- ✅ 检测 1：哈希自指检测
SELECT
    '✅ Hash Self-Reference' as check,
    (SELECT COUNT(*) FROM blocks WHERE hash = parent_hash) as issues,
    CASE
        WHEN (SELECT COUNT(*) FROM blocks WHERE hash = parent_hash) = 0
        THEN 'PASS'
        ELSE 'FAIL'
    END as status;

-- ✅ 检测 2：链完整性检测
SELECT
    '✅ Chain Integrity' as check,
    (SELECT COUNT(*) FROM blocks b
     WHERE b.parent_hash IN (SELECT hash FROM blocks WHERE hash IS NOT NULL)
    ) as linked_blocks,
    (SELECT COUNT(*) FROM blocks) as total_blocks,
    CASE
        WHEN (SELECT COUNT(*) FROM blocks) =
             (SELECT COUNT(*) FROM blocks b WHERE b.parent_hash IN (SELECT hash FROM blocks WHERE hash IS NOT NULL))
        THEN 'PASS'
        ELSE 'FAIL'
    END as status;

-- ✅ 检测 3：重复块检测
SELECT
    '✅ Duplicate Blocks' as check,
    (SELECT COUNT(*) FROM blocks) - (SELECT COUNT(DISTINCT number) FROM blocks) as issues,
    CASE
        WHEN (SELECT COUNT(*) FROM blocks) = (SELECT COUNT(DISTINCT number) FROM blocks)
        THEN 'PASS'
        ELSE 'FAIL'
    END as status;

-- ✅ 检测 4：Gap 检测
SELECT
    '✅ Gap Detection' as check,
    (MAX(number) - MIN(number) + 1) - COUNT(*) as gaps,
    CASE
        WHEN (MAX(number) - MIN(number) + 1) - COUNT(*) = 0
        THEN 'PASS (Sequential)'
        ELSE 'WARN (Has Gaps)'
    END as status
FROM blocks;
