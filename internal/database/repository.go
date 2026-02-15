package database

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/jmoiron/sqlx"

	_ "github.com/jackc/pgx/v5/stdlib" // Required for sqlx Open to recognize "pgx" driver
)

type Repository struct {
	db *sqlx.DB
}

func NewRepository(databaseURL string) (*Repository, error) {
	db, err := sqlx.Connect("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 配置连接池 - 生产级设置
	db.SetMaxOpenConns(25)                 // 最大打开连接数
	db.SetMaxIdleConns(10)                 // 最大空闲连接数
	db.SetConnMaxLifetime(5 * time.Minute) // 连接最大生命周期
	db.SetConnMaxIdleTime(1 * time.Minute) // 空闲连接最大存活时间

	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

// SaveBlock 插入区块.
func (r *Repository) SaveBlock(ctx context.Context, block *models.Block) error {
	query := `
		INSERT INTO blocks (number, hash, parent_hash, timestamp)
		VALUES (:number, :hash, :parent_hash, :timestamp)
		ON CONFLICT (number) DO NOTHING
	`
	_, err := r.db.NamedExecContext(ctx, query, block)
	return err
}

// SaveTransfer 插入Transfer事件.
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

// GetLatestBlockNumber 获取已处理的最高区块.
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

// UpdateCheckpoint 更新同步进度.
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

// UpdateSyncStatus 更新同步状态（持久性检查点）.
func (r *Repository) UpdateSyncStatus(ctx context.Context, chainID int64, lastProcessedBlock *big.Int, rpcProvider string) error {
	query := `
		INSERT INTO sync_status (chain_id, last_processed_block, rpc_provider, status)
		VALUES ($1, $2, $3, 'syncing')
		ON CONFLICT (chain_id) DO UPDATE SET 
			last_processed_block = EXCLUDED.last_processed_block,
			last_processed_timestamp = NOW(),
			rpc_provider = EXCLUDED.rpc_provider,
			status = 'syncing',
			error_message = NULL
	`
	_, err := r.db.ExecContext(ctx, query, chainID, lastProcessedBlock.String(), rpcProvider)
	return err
}

// GetSyncStatus 获取同步状态
func (r *Repository) GetSyncStatus(ctx context.Context, chainID int64) (*big.Int, string, error) {
	var lastProcessedBlock string
	var rpcProvider string
	query := `SELECT last_processed_block, rpc_provider FROM sync_status WHERE chain_id = $1`
	err := r.db.GetContext(ctx, &struct {
		LastProcessedBlock string
		RPCProvider        string
	}{}, query, chainID)
	if err != nil {
		// 如果表为空，返回 nil（让调用者使用 checkpoint）
		return nil, "", nil
	}
	blockNum := new(big.Int)
	blockNum.SetString(lastProcessedBlock, 10)
	return blockNum, rpcProvider, nil
}

// RecordSyncError 记录同步错误
func (r *Repository) RecordSyncError(ctx context.Context, chainID int64, errorMsg string) error {
	query := `
		UPDATE sync_status 
		SET status = 'error', error_message = $2, last_processed_timestamp = NOW()
		WHERE chain_id = $1
	`
	_, err := r.db.ExecContext(ctx, query, chainID, errorMsg)
	return err
}
