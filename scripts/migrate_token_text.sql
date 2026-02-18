-- Migration: Change token_metadata columns to TEXT for maximum flexibility
-- Date: 2026-02-18
-- Issue: VARCHAR(100) still insufficient for some DeFi protocol tokens
-- Solution: Use TEXT for unlimited length (with performance trade-off)

-- Step 1: Alert user
SELECT 'Starting migration: VARCHAR(100) → TEXT' AS notice;

-- Step 2: Alter columns to TEXT
ALTER TABLE token_metadata ALTER COLUMN symbol TYPE TEXT;

-- Step 3: Verify the change
\d token_metadata

-- Expected output:
--  Column   |         Type          | Collation | Nullable | Default
-- ----------+-----------------------+-----------+----------+---------
-- address   | character varying(42)|           | not null |
-- symbol    | text                  |           | not null |  ← Now TEXT!
-- decimals  | smallint             |           | not null | 18
-- name      | text                 |           |          |
-- ...
