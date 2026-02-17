-- migrations/003_add_activity_type.sql

-- 1. 为 transfers 表添加 symbol 字段（如果之前只是在 models 中添加但数据库里还没同步）
-- 注意：之前的 commits 可能已经手动添加过这个字段，所以使用 IF NOT EXISTS 或先检查
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS WHERE table_name = 'transfers' AND column_name = 'symbol') THEN
        ALTER TABLE transfers ADD COLUMN symbol VARCHAR(20);
    END IF;
END $$;

-- 2. 添加 activity_type 字段，用于区分 Transfer, Swap, Mint 等
ALTER TABLE transfers ADD COLUMN IF NOT EXISTS activity_type VARCHAR(20) DEFAULT 'TRANSFER';

-- 3. 添加索引
CREATE INDEX IF NOT EXISTS idx_transfers_activity_type ON transfers(activity_type);
