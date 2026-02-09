package engine

import (
	"context"
	"database/sql"
	"fmt"

	"web3-indexer-go/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jmoiron/sqlx"
)

type execer interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

// BulkInserter 使用 PostgreSQL COPY 协议进行高效批量插入
type BulkInserter struct {
	db *sqlx.DB
}

func NewBulkInserter(db *sqlx.DB) *BulkInserter {
	return &BulkInserter{db: db}
}

// InsertBlocksBatch 使用 COPY 批量插入区块（比 INSERT 快 10-100 倍）
func (b *BulkInserter) InsertBlocksBatch(ctx context.Context, blocks []models.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	// 获取底层 pgx 连接
	conn, err := b.db.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get raw conn: %w", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn interface{}) error {
		pgxConn, ok := driverConn.(*pgx.Conn)
		if !ok {
			// 回退到普通批量 INSERT
			return b.fallbackInsertBlocks(ctx, b.db, blocks)
		}

		// 使用 COPY FROM 高效插入
		_, err := pgxConn.CopyFrom(
			ctx,
			pgx.Identifier{"blocks"},
			[]string{"number", "hash", "parent_hash", "timestamp"},
			pgx.CopyFromSlice(len(blocks), func(i int) ([]interface{}, error) {
				return []interface{}{
					blocks[i].Number.String(),
					blocks[i].Hash,
					blocks[i].ParentHash,
					int64(blocks[i].Timestamp),
				}, nil
			}),
		)
		return err
	})

	if err != nil {
		return fmt.Errorf("batch insert blocks failed: %w", err)
	}
	return nil
}

// InsertTransfersBatch 使用 COPY 批量插入转账事件
func (b *BulkInserter) InsertTransfersBatch(ctx context.Context, transfers []models.Transfer) error {
	if len(transfers) == 0 {
		return nil
	}

	conn, err := b.db.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get raw conn: %w", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn interface{}) error {
		pgxConn, ok := driverConn.(*pgx.Conn)
		if !ok {
			return b.fallbackInsertTransfers(ctx, b.db, transfers)
		}

		_, err := pgxConn.CopyFrom(
			ctx,
			pgx.Identifier{"transfers"},
			[]string{"block_number", "tx_hash", "log_index", "from_address", "to_address", "amount", "token_address"},
			pgx.CopyFromSlice(len(transfers), func(i int) ([]interface{}, error) {
				return []interface{}{
					transfers[i].BlockNumber.String(),
					transfers[i].TxHash,
					int64(transfers[i].LogIndex),
					transfers[i].From,
					transfers[i].To,
					transfers[i].Amount.String(),
					transfers[i].TokenAddress,
				}, nil
			}),
		)
		return err
	})

	if err != nil {
		return fmt.Errorf("batch insert transfers failed: %w", err)
	}
	return nil
}

// fallbackInsertBlocks 当 COPY 不可用时回退到批量 INSERT
func (b *BulkInserter) fallbackInsertBlocks(ctx context.Context, exec execer, blocks []models.Block) error {
	// 使用unnest批量插入
	numbers := make([]string, len(blocks))
	hashes := make([]string, len(blocks))
	parentHashes := make([]string, len(blocks))
	timestamps := make([]int64, len(blocks))

	for i, b := range blocks {
		numbers[i] = b.Number.String()
		hashes[i] = b.Hash
		parentHashes[i] = b.ParentHash
		timestamps[i] = int64(b.Timestamp)
	}

	query := `
		INSERT INTO blocks (number, hash, parent_hash, timestamp)
		SELECT * FROM UNNEST($1::numeric[], $2::text[], $3::text[], $4::bigint[])
		ON CONFLICT (number) DO NOTHING
	`
	_, err := exec.ExecContext(ctx, query, numbers, hashes, parentHashes, timestamps)
	return err
}

// fallbackInsertTransfers 当 COPY 不可用时回退到批量 INSERT
func (b *BulkInserter) fallbackInsertTransfers(ctx context.Context, exec execer, transfers []models.Transfer) error {
	blockNumbers := make([]string, len(transfers))
	txHashes := make([]string, len(transfers))
	logIndices := make([]uint64, len(transfers))
	froms := make([]string, len(transfers))
	tos := make([]string, len(transfers))
	amounts := make([]string, len(transfers))
	tokenAddresses := make([]string, len(transfers))

	for i, t := range transfers {
		blockNumbers[i] = t.BlockNumber.String()
		txHashes[i] = t.TxHash
		logIndices[i] = uint64(t.LogIndex)
		froms[i] = t.From
		tos[i] = t.To
		amounts[i] = t.Amount.String()
		tokenAddresses[i] = t.TokenAddress
	}

	query := `
		INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
		SELECT * FROM UNNEST($1::numeric[], $2::text[], $3::int[], $4::text[], $5::text[], $6::numeric[], $7::text[])
		ON CONFLICT (block_number, log_index) DO NOTHING
	`
	_, err := exec.ExecContext(ctx, query, blockNumbers, txHashes, logIndices, froms, tos, amounts, tokenAddresses)
	return err
}

// InsertBlocksBatchTx 批量插入区块并在给定事务内执行
func (b *BulkInserter) InsertBlocksBatchTx(ctx context.Context, exec execer, blocks []models.Block) error {
	if len(blocks) == 0 {
		return nil
	}
	return b.fallbackInsertBlocks(ctx, exec, blocks)
}

// InsertTransfersBatchTx 批量插入转账事件并在给定事务内执行
func (b *BulkInserter) InsertTransfersBatchTx(ctx context.Context, exec execer, transfers []models.Transfer) error {
	if len(transfers) == 0 {
		return nil
	}
	return b.fallbackInsertTransfers(ctx, exec, transfers)
}
