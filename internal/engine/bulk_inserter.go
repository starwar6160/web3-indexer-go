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
			[]string{"number", "hash", "parent_hash", "timestamp", "gas_limit", "gas_used", "base_fee_per_gas", "transaction_count"},
			pgx.CopyFromSlice(len(blocks), func(i int) ([]interface{}, error) {
				var baseFee *string
				if blocks[i].BaseFeePerGas != nil {
					s := blocks[i].BaseFeePerGas.String()
					baseFee = &s
				}
				return []interface{}{
					blocks[i].Number.String(),
					blocks[i].Hash,
					blocks[i].ParentHash,
					blocks[i].Timestamp,
					blocks[i].GasLimit,
					blocks[i].GasUsed,
					baseFee,
					blocks[i].TransactionCount,
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
			[]string{"block_number", "tx_hash", "log_index", "from_address", "to_address", "amount", "token_address", "symbol"}, // ✅ 添加 symbol
			pgx.CopyFromSlice(len(transfers), func(i int) ([]interface{}, error) {
				return []interface{}{
					transfers[i].BlockNumber.String(),
					transfers[i].TxHash,
					transfers[i].LogIndex,
					transfers[i].From,
					transfers[i].To,
					transfers[i].Amount.String(),
					transfers[i].TokenAddress,
					transfers[i].Symbol, // ✅ 添加 Symbol
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
	gasLimits := make([]int64, len(blocks))
	gasUseds := make([]int64, len(blocks))
	baseFees := make([]*string, len(blocks))
	txCounts := make([]int, len(blocks))

	for i, b := range blocks {
		numbers[i] = b.Number.String()
		hashes[i] = b.Hash
		parentHashes[i] = b.ParentHash
		// #nosec G115 - Ethereum timestamps and gas limits fit in int64
		timestamps[i] = int64(b.Timestamp)
		// #nosec G115
		gasLimits[i] = int64(b.GasLimit)
		// #nosec G115
		gasUseds[i] = int64(b.GasUsed)
		if b.BaseFeePerGas != nil {
			s := b.BaseFeePerGas.String()
			baseFees[i] = &s
		}
		txCounts[i] = b.TransactionCount
	}

	query := `
		INSERT INTO blocks (number, hash, parent_hash, timestamp, gas_limit, gas_used, base_fee_per_gas, transaction_count)
		SELECT * FROM UNNEST($1::numeric[], $2::text[], $3::text[], $4::bigint[], $5::bigint[], $6::bigint[], $7::numeric[], $8::int[])
		ON CONFLICT (number) DO NOTHING
	`
	_, err := exec.ExecContext(ctx, query, numbers, hashes, parentHashes, timestamps, gasLimits, gasUseds, baseFees, txCounts)
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
	symbols := make([]string, len(transfers)) // ✅ 新增：Symbol 数组

	for i, t := range transfers {
		blockNumbers[i] = t.BlockNumber.String()
		txHashes[i] = t.TxHash
		logIndices[i] = uint64(t.LogIndex)
		froms[i] = t.From
		tos[i] = t.To
		amounts[i] = t.Amount.String()
		tokenAddresses[i] = t.TokenAddress
		symbols[i] = t.Symbol // ✅ 新增：Symbol 赋值
	}

	query := `
		INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol)
		SELECT * FROM UNNEST($1::numeric[], $2::text[], $3::int[], $4::text[], $5::text[], $6::numeric[], $7::text[], $8::text[])
		ON CONFLICT (block_number, log_index) DO NOTHING
	`
	_, err := exec.ExecContext(ctx, query, blockNumbers, txHashes, logIndices, froms, tos, amounts, tokenAddresses, symbols)
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
