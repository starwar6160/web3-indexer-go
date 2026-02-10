-- Web3 Indexer 数据库初始化脚本
-- 创建必要的表结构

-- 区块表
CREATE TABLE IF NOT EXISTS blocks (
    hash VARCHAR(66) PRIMARY KEY,
    number BIGINT UNIQUE NOT NULL,
    parent_hash VARCHAR(66) NOT NULL,
    timestamp BIGINT NOT NULL,
    gas_limit BIGINT NOT NULL,
    gas_used BIGINT NOT NULL,
    base_fee_per_gas NUMERIC(78, 0),
    transaction_count INTEGER NOT NULL,
    processed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 交易表
CREATE TABLE IF NOT EXISTS transactions (
    hash VARCHAR(66) PRIMARY KEY,
    block_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    transaction_index INTEGER NOT NULL,
    from_address VARCHAR(42) NOT NULL,
    to_address VARCHAR(42),
    value NUMERIC(36, 0) NOT NULL,
    gas_limit BIGINT NOT NULL,
    gas_used BIGINT NOT NULL,
    gas_price BIGINT,
    max_fee_per_gas BIGINT,
    max_priority_fee_per_gas BIGINT,
    nonce BIGINT NOT NULL,
    input_data TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (block_hash) REFERENCES blocks(hash)
);

-- 日志表
CREATE TABLE IF NOT EXISTS logs (
    id SERIAL PRIMARY KEY,
    transaction_hash VARCHAR(66) NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    log_index INTEGER NOT NULL,
    address VARCHAR(42) NOT NULL,
    topics TEXT[] NOT NULL,
    data TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (transaction_hash) REFERENCES transactions(hash),
    FOREIGN KEY (block_hash) REFERENCES blocks(hash)
);

-- 转账事件表 (ERC20 Transfer事件的解析结果)
CREATE TABLE IF NOT EXISTS transfers (
    id SERIAL PRIMARY KEY,
    tx_hash CHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    log_index INTEGER NOT NULL,
    token_address CHAR(42) NOT NULL,
    from_address CHAR(42) NOT NULL,
    to_address CHAR(42) NOT NULL,
    amount NUMERIC(78, 0) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(block_number, log_index)
);

-- 同步检查点表
CREATE TABLE IF NOT EXISTS sync_checkpoints (
    chain_id BIGINT PRIMARY KEY,
    last_synced_block VARCHAR(20) NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 同步状态表 (更详细的同步信息)
CREATE TABLE IF NOT EXISTS sync_status (
    chain_id BIGINT PRIMARY KEY,
    last_synced_block BIGINT NOT NULL,
    latest_block BIGINT NOT NULL,
    sync_lag BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'syncing', -- syncing, synced, error
    error_message TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引以提高查询性能
CREATE INDEX IF NOT EXISTS idx_blocks_number ON blocks(number);
CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks(timestamp);
CREATE INDEX IF NOT EXISTS idx_transactions_block_number ON transactions(block_number);
CREATE INDEX IF NOT EXISTS idx_transactions_from_address ON transactions(from_address);
CREATE INDEX IF NOT EXISTS idx_transactions_to_address ON transactions(to_address);
CREATE INDEX IF NOT EXISTS idx_logs_block_number ON logs(block_number);
CREATE INDEX IF NOT EXISTS idx_logs_address ON logs(address);
CREATE INDEX IF NOT EXISTS idx_logs_transaction_hash ON logs(transaction_hash);
CREATE INDEX IF NOT EXISTS idx_transfers_block_number ON transfers(block_number);
CREATE INDEX IF NOT EXISTS idx_transfers_token_address ON transfers(token_address);
CREATE INDEX IF NOT EXISTS idx_transfers_from_address ON transfers(from_address);
CREATE INDEX IF NOT EXISTS idx_transfers_to_address ON transfers(to_address);
CREATE INDEX IF NOT EXISTS idx_transfers_from_to ON transfers(from_address, to_address);

-- 插入默认检查点 (以太坊主网从区块 0 开始)
INSERT INTO sync_checkpoints (chain_id, last_synced_block) 
VALUES (1, '0') 
ON CONFLICT (chain_id) DO NOTHING;

-- 插入默认同步状态
INSERT INTO sync_status (chain_id, last_synced_block, latest_block, sync_lag, status) 
VALUES (1, 0, 0, 0, 'syncing') 
ON CONFLICT (chain_id) DO NOTHING;
