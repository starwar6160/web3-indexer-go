-- migrations/004_token_metadata.sql
-- L2 Cache for Token Metadata to save RPC Quota
CREATE TABLE IF NOT EXISTS token_metadata (
    address VARCHAR(42) PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    decimals SMALLINT NOT NULL DEFAULT 18,
    name TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for faster lookups
CREATE INDEX IF NOT EXISTS idx_token_metadata_symbol ON token_metadata(symbol);
