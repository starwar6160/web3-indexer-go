-- =============================================================================
-- Web3 Indexer - 区块处理耗时统计
-- =============================================================================
-- 用途：统计每 100 个区块的平均写入延迟，评估系统性能
-- 目标：确保 PostgreSQL 写入性能不是瓶颈
-- =============================================================================

-- 查询 1：最新 100 个区块的处理时间分布
SELECT
  'Latest 100 Blocks Processing Time' AS metric,
  COUNT(*) AS total_blocks,
  MIN(processed_at - timestamp) AS min_latency,
  MAX(processed_at - timestamp) AS max_latency,
  AVG(processed_at - timestamp) AS avg_latency,
  percentile_cont(0.5) WITHIN GROUP (ORDER BY processed_at - timestamp) AS p50_latency,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY processed_at - timestamp) AS p95_latency,
  percentile_cont(0.99) WITHIN GROUP (ORDER BY processed_at - timestamp) AS p99_latency
FROM blocks
WHERE number >= (
  SELECT MAX(number) - 99 FROM blocks
)
ORDER BY number DESC;

-- 查询 2：按时间窗口统计（每 100 个区块为一组）
SELECT
  FLOOR(number / 100) AS batch_group,
  MIN(number) AS start_block,
  MAX(number) AS end_block,
  COUNT(*) AS blocks_in_batch,
  AVG(EXTRACT(EPOCH FROM (processed_at - timestamp))) AS avg_latency_seconds,
  MIN(EXTRACT(EPOCH FROM (processed_at - timestamp))) AS min_latency_seconds,
  MAX(EXTRACT(EPOCH FROM (processed_at - timestamp))) AS max_latency_seconds
FROM blocks
WHERE number >= (
  SELECT MAX(number) - 999 FROM blocks  -- 最近 1000 个块，分 10 组
)
GROUP BY FLOOR(number / 100)
ORDER BY batch_group DESC;

-- 查询 3：识别处理时间异常的区块
SELECT
  number AS block_number,
  to_char(timestamp, 'HH24:MI:SS') AS block_timestamp,
  to_char(processed_at, 'HH24:MI:SS') AS processed_at,
  EXTRACT(EPOCH FROM (processed_at - timestamp)) AS latency_seconds,
  gas_used,
  tx_count
FROM blocks
WHERE number >= (
  SELECT MAX(number) - 99 FROM blocks
)
  AND EXTRACT(EPOCH FROM (processed_at - timestamp)) > 5  -- 超过 5 秒视为异常
ORDER BY latency_seconds DESC
LIMIT 10;

-- 查询 4：写入吞吐量（每秒写入的区块数）
SELECT
  'Write Throughput (blocks/sec)' AS metric,
  COUNT(*) AS blocks_processed,
  EXTRACT(EPOCH FROM (MAX(processed_at) - MIN(processed_at))) AS time_window_seconds,
  COUNT(*) / NULLIF(EXTRACT(EPOCH FROM (MAX(processed_at) - MIN(processed_at))), 0) AS blocks_per_second
FROM blocks
WHERE number >= (
  SELECT MAX(number) - 99 FROM blocks
);

-- 查询 5：Transfer 写入性能（每 100 个区块对应的 Transfer）
SELECT
  'Transfer Write Performance' AS metric,
  COUNT(DISTINCT b.number) AS block_count,
  COUNT(t.id) AS transfer_count,
  COUNT(t.id) / NULLIF(COUNT(DISTINCT b.number), 0) AS avg_transfers_per_block,
  AVG(EXTRACT(EPOCH FROM (b.processed_at - b.timestamp))) AS avg_block_latency_seconds
FROM blocks b
LEFT JOIN transfers t ON b.number = t.block_number
WHERE b.number >= (
  SELECT MAX(b.number) - 99 FROM blocks b
);

-- 查询 6：数据库连接池使用情况（如果启用了 pg_stat_statements）
SELECT
  calls,
  total_exec_time,
  mean_exec_time,
  max_exec_time,
  stddev_exec_time
FROM pg_stat_statements
WHERE queryid IN (
  SELECT queryid
  FROM pg_stat_statements
  WHERE query LIKE '%INSERT INTO blocks%'
     OR query LIKE '%INSERT INTO transfers%'
)
ORDER BY mean_exec_time DESC
LIMIT 5;

-- 查询 7：磁盘 I/O 统计（PostgreSQL 级别）
SELECT
  datname,
  numbackends,
  xact_commit,
  blks_read,
  blks_hit,
  round(blks_hit::numeric / NULLIF(blks_hit + blks_read, 0) * 100, 2) AS cache_hit_ratio
FROM pg_stat_database
WHERE datname = 'web3_sepolia';

-- 查询 8：表大小增长趋势（每 100 个区块）
SELECT
  'blocks' AS table_name,
  pg_size_pretty(pg_total_relation_size('blocks')) AS total_size,
  (SELECT COUNT(*) FROM blocks) AS row_count
UNION ALL
SELECT
  'transfers' AS table_name,
  pg_size_pretty(pg_total_relation_size('transfers')) AS total_size,
  (SELECT COUNT(*) FROM transfers) AS row_count;

-- 查询 9：最慢的 10 个区块处理记录（Top 10 Slowest Blocks）
SELECT
  number AS block_number,
  to_char(timestamp, 'YYYY-MM-DD HH24:MI:SS') AS block_time,
  to_char(processed_at, 'YYYY-MM-DD HH24:MI:SS') AS processed_time,
  EXTRACT(EPOCH FROM (processed_at - timestamp)) AS latency_seconds,
  gas_used,
  tx_count,
  substring(hash, 1, 10) AS hash_prefix
FROM blocks
ORDER BY EXTRACT(EPOCH FROM (processed_at - timestamp)) DESC
LIMIT 10;

-- 查询 10：处理时间趋势（时间序列）
SELECT
  date_trunc('minute', processed_at) AS time_bucket,
  COUNT(*) AS blocks_processed,
  AVG(EXTRACT(EPOCH FROM (processed_at - timestamp))) AS avg_latency_seconds,
  MAX(EXTRACT(EPOCH FROM (processed_at - timestamp))) AS max_latency_seconds
FROM blocks
WHERE processed_at > NOW() - INTERVAL '1 hour'
GROUP BY date_trunc('minute', processed_at)
ORDER BY time_bucket DESC
LIMIT 20;

-- =============================================================================
-- 快速验证查询（用于面试演示）
-- =============================================================================

-- 快速查询 1：最新 100 个块的平均延迟
SELECT
  ROUND(AVG(EXTRACT(EPOCH FROM (processed_at - timestamp)))::numeric, 2) AS avg_latency_seconds
FROM blocks
WHERE number >= (SELECT MAX(number) - 99 FROM blocks);

-- 快速查询 2：识别慢查询（> 1 秒）
SELECT COUNT(*) AS slow_blocks_count
FROM blocks
WHERE EXTRACT(EPOCH FROM (processed_at - timestamp)) > 1
  AND number >= (SELECT MAX(number) - 99 FROM blocks);

-- 快速查询 3：数据库缓存命中率
SELECT
  ROUND(blks_hit::numeric / NULLIF(blks_hit + blks_read, 0) * 100, 2) AS cache_hit_ratio_percentage
FROM pg_stat_database
WHERE datname = 'web3_sepolia';

-- =============================================================================
-- 使用示例
-- =============================================================================

-- 运行完整统计：
-- docker exec web3-indexer-sepolia-db psql -U postgres -d web3_sepolia -f /scripts/sql-block-processing-latency.sql

-- 运行单个查询：
-- docker exec web3-indexer-sepolia-db psql -U postgres -d web3_sepolia -c "
--   SELECT ROUND(AVG(EXTRACT(EPOCH FROM (processed_at - timestamp)))::numeric, 2) AS avg_latency_seconds
--   FROM blocks
--   WHERE number >= (SELECT MAX(number) - 99 FROM blocks);
-- "

-- =============================================================================
-- 预期结果（健康系统）
-- =============================================================================

-- avg_latency_seconds: < 1 秒
-- p95_latency: < 2 秒
-- p99_latency: < 5 秒
-- cache_hit_ratio_percentage: > 99%
-- blocks_per_second: > 0.1 (取决于 QPS 配置)
