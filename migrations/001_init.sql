-- migrations/001_init.sql

CREATE TABLE IF NOT EXISTS blocks (
    number NUMERIC(78,0) PRIMARY KEY, -- 对应 uint256
    hash CHAR(66) NOT NULL UNIQUE,
    parent_hash CHAR(66) NOT NULL,
    timestamp NUMERIC(20,0) NOT NULL, -- 支持毫秒级
    processed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_blocks_parent_hash ON blocks(parent_hash);

CREATE TABLE IF NOT EXISTS transfers (
    id SERIAL PRIMARY KEY,
    block_number NUMERIC(78,0) NOT NULL REFERENCES blocks(number) ON DELETE CASCADE,
    tx_hash CHAR(66) NOT NULL,
    log_index INTEGER NOT NULL,
    from_address CHAR(42) NOT NULL,
    to_address CHAR(42) NOT NULL,
    amount NUMERIC(78,0) NOT NULL, -- 严禁使用 FLOAT/DOUBLE
    token_address CHAR(42) NOT NULL,
    UNIQUE(block_number, log_index)
);

CREATE TABLE IF NOT EXISTS sync_checkpoints (
    id SERIAL PRIMARY KEY,
    chain_id NUMERIC(78,0) UNIQUE,
    last_synced_block NUMERIC(78,0) NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
