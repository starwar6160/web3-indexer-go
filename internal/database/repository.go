package database

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
	"web3-indexer-go/internal/models"
)

type Repository struct {
	db *sqlx.DB
}

func NewRepository(databaseURL string) (*Repository, error) {
	db, err := sqlx.Connect("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

// SaveBlock 插入区块
func (r *Repository) SaveBlock(ctx context.Context, block *models.Block) error {
	query := `
		INSERT INTO blocks (number, hash, parent_hash, timestamp)
		VALUES (:number, :hash, :parent_hash, :timestamp)
		ON CONFLICT (number) DO NOTHING
	`
	_, err := r.db.NamedExecContext(ctx, query, block)
	return err
}

// SaveTransfer 插入Transfer事件
func (r *Repository) SaveTransfer(ctx context.Context, transfer *models.Transfer) error {
	query := `
		INSERT INTO transfers 
		(block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
		VALUES 
		(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address)
		ON CONFLICT (block_number, log_index) DO NOTHING
	`
	_, err := r.db.NamedExecContext(ctx, query, transfer)
	return err
}

// GetLatestBlockNumber 获取已处理的最高区块
func (r *Repository) GetLatestBlockNumber(ctx context.Context, chainID int64) (*big.Int, error) {
	var number string
	err := r.db.GetContext(ctx, &number, 
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
	if err != nil {
		log.Printf("No checkpoint found for chain %d, starting from 0", chainID)
		return big.NewInt(0), nil
	}
	n, ok := new(big.Int).SetString(number, 10)
	if !ok {
		return nil, fmt.Errorf("invalid block number in database: %s", number)
	}
	return n, nil
}

// UpdateCheckpoint 更新同步进度
func (r *Repository) UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error {
	query := `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET 
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()
	`
	_, err := r.db.ExecContext(ctx, query, chainID, blockNumber.String())
	return err
}
