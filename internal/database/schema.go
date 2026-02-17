package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// InitSchema 确保数据库核心表结构已就绪
func InitSchema(ctx context.Context, db *sqlx.DB) error {
	slog.Info("🛡️ [Database] Initializing Schema...")

	schema := `
	CREATE TABLE IF NOT EXISTS blocks (
		number NUMERIC PRIMARY KEY,
		hash VARCHAR(66) NOT NULL,
		parent_hash VARCHAR(66) NOT NULL DEFAULT '',
		timestamp BIGINT NOT NULL,
		gas_limit BIGINT DEFAULT 0,
		gas_used BIGINT DEFAULT 0,
		base_fee_per_gas NUMERIC,
		transaction_count INTEGER DEFAULT 0,
		processed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS transfers (
		id SERIAL PRIMARY KEY,
		block_number NUMERIC NOT NULL REFERENCES blocks(number) ON DELETE CASCADE,
		tx_hash VARCHAR(66) NOT NULL,
		log_index INTEGER NOT NULL,
		from_address VARCHAR(42) NOT NULL,
		to_address VARCHAR(42) NOT NULL,
		amount NUMERIC NOT NULL,
		token_address VARCHAR(42) NOT NULL,
		symbol VARCHAR(20),
		activity_type VARCHAR(20) DEFAULT 'TRANSFER',
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS token_metadata (
		address VARCHAR(42) PRIMARY KEY,
		symbol VARCHAR(20) NOT NULL,
		decimals SMALLINT NOT NULL DEFAULT 18,
		name TEXT,
		is_verified BOOLEAN DEFAULT FALSE,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS sync_checkpoints (
		chain_id BIGINT PRIMARY KEY,
		last_synced_block NUMERIC NOT NULL,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS sync_status (
		chain_id BIGINT PRIMARY KEY,
		last_processed_block NUMERIC NOT NULL,
		last_processed_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		rpc_provider TEXT,
		status VARCHAR(20),
		error_message TEXT
	);

	CREATE TABLE IF NOT EXISTS visitor_stats (
		id SERIAL PRIMARY KEY,
		ip_address VARCHAR(45) NOT NULL,
		user_agent TEXT,
		visit_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		metadata JSONB
	);
	`

	_, err := db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	// 🚀 工业级补丁：确保旧表结构也能对齐最新逻辑
	patches := []string{
		"ALTER TABLE sync_status ADD COLUMN IF NOT EXISTS last_processed_block NUMERIC",
		"ALTER TABLE sync_status ADD COLUMN IF NOT EXISTS last_processed_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW()",
		"ALTER TABLE transfers ADD COLUMN IF NOT EXISTS activity_type VARCHAR(20) DEFAULT 'TRANSFER'",
	}
	for _, patch := range patches {
		_, _ = db.ExecContext(ctx, patch)
	}

	// 补充索引
	indices := []string{
		"CREATE INDEX IF NOT EXISTS idx_transfers_block_number ON transfers(block_number)",
		"CREATE INDEX IF NOT EXISTS idx_transfers_tx_hash ON transfers(tx_hash)",
		"CREATE INDEX IF NOT EXISTS idx_token_metadata_symbol ON token_metadata(symbol)",
	}

	for _, idx := range indices {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			slog.Warn("failed_to_create_index", "err", err)
		}
	}

	slog.Info("✅ [Database] Schema is ready.")
	return nil
}
