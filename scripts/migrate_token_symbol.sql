-- Migration: Expand token_metadata.symbol to support long token symbols
-- Date: 2026-02-18
-- Issue: SQLSTATE 22001 - value too long for type character varying(20)

-- Step 1: Backup existing data (optional, for safety)
-- CREATE TABLE token_metadata_backup AS SELECT * FROM token_metadata;

-- Step 2: Alter column to VARCHAR(100)
ALTER TABLE token_metadata ALTER COLUMN symbol TYPE VARCHAR(100);

-- Step 3: Verify the change
\d token_metadata

-- Expected output:
--  Column   |         Type          | Collation | Nullable | Default
-- ----------+-----------------------+-----------+----------+---------
-- address   | character varying(42)|           | not null |
-- symbol    | character varying(100)|           | not null |  ← Expanded!
-- decimals  | smallint             |           | not null | 18
-- name      | text                 |           |          |
-- ...
