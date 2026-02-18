package engine

import (
	"context"
	"fmt"
	"web3-indexer-go/internal/models"

	"github.com/jmoiron/sqlx"
)

// PostgresSink 数据库消费者实现
type PostgresSink struct {
	db *sqlx.DB
}

func NewPostgresSink(db *sqlx.DB) *PostgresSink {
	return &PostgresSink{db: db}
}

func (s *PostgresSink) WriteTransfers(ctx context.Context, transfers []models.Transfer) error {
	if len(transfers) == 0 {
		return nil
	}

	// 采用批处理写入以压榨普通 SSD 性能
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO transfers 
		(block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
		VALUES 
		(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
		ON CONFLICT (block_number, log_index) DO NOTHING
	`, transfers)

	if err != nil {
		return fmt.Errorf("postgres_sink_transfer_failed: %w", err)
	}
	return nil
}

func (s *PostgresSink) WriteBlocks(ctx context.Context, blocks []models.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	inserter := NewBulkInserter(s.db)
	// 利用现有的 BulkInserter 实现高效区块入库
	return inserter.InsertBlocksBatch(ctx, blocks)
}

func (s *PostgresSink) Close() error {
	// 数据库连接通常由外部管理，这里仅作接口对齐
	return nil
}
