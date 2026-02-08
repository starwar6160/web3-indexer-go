package engine

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseConnection 测试数据库连接
func TestDatabaseConnection(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err, "failed to connect to database")
	defer db.Close()

	// 测试连接
	err = db.Ping()
	require.NoError(t, err, "database ping failed")

	t.Logf("✅ Successfully connected to database")
}

// TestTransactionalBlockSave 测试事务原子性
func TestTransactionalBlockSave(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// 清理测试数据
	_, _ = db.ExecContext(ctx, "DELETE FROM transfers WHERE block_number = 999999")
	_, _ = db.ExecContext(ctx, "DELETE FROM blocks WHERE number = 999999")

	// 开始事务
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// 插入测试区块
	blockHash := common.HexToHash("0x1234567890abcdef")
	blockNumber := big.NewInt(999999)

	_, err = tx.ExecContext(ctx,
		`INSERT INTO blocks (hash, number, timestamp, gas_limit, gas_used, transaction_count)
		 VALUES ($1, $2, NOW(), $3, $4, $5)`,
		blockHash.Hex(), blockNumber.String(), int64(21000), int64(21000), 1,
	)
	require.NoError(t, err, "failed to insert block")

	// 插入测试转账
	_, err = tx.ExecContext(ctx,
		`INSERT INTO transfers (transaction_hash, block_number, log_index, token_address, from_address, to_address, value)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		common.HexToHash("0xaaaa").Hex(), blockNumber.String(), 0,
		common.HexToAddress("0x1111").Hex(),
		common.HexToAddress("0x2222").Hex(),
		common.HexToAddress("0x3333").Hex(),
		"1000000000000000000",
	)
	require.NoError(t, err, "failed to insert transfer")

	// 回滚事务
	err = tx.Rollback()
	require.NoError(t, err)

	// 验证数据未被写入
	var count int
	err = db.GetContext(ctx, &count, "SELECT COUNT(*) FROM blocks WHERE number = 999999")
	require.NoError(t, err)
	assert.Equal(t, 0, count, "block should not exist after rollback")

	t.Logf("✅ Transaction rollback verified: data was not persisted")
}

// TestCheckpointPersistence 测试Checkpoint持久化
func TestCheckpointPersistence(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	chainID := int64(11155111) // Sepolia

	// 清理旧数据
	_, _ = db.ExecContext(ctx, "DELETE FROM sync_checkpoints WHERE chain_id = $1", chainID)

	// 插入Checkpoint
	testBlock := "10216000"
	_, err = db.ExecContext(ctx,
		"INSERT INTO sync_checkpoints (chain_id, last_synced_block) VALUES ($1, $2)",
		chainID, testBlock,
	)
	require.NoError(t, err, "failed to insert checkpoint")

	// 读取Checkpoint
	var lastSyncedBlock string
	err = db.GetContext(ctx, &lastSyncedBlock,
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1",
		chainID,
	)
	require.NoError(t, err, "failed to read checkpoint")
	assert.Equal(t, testBlock, lastSyncedBlock, "checkpoint value should match")

	t.Logf("✅ Checkpoint persistence verified: %s", lastSyncedBlock)
}

// TestMultipleTransfers 测试多笔转账的批量插入
func TestMultipleTransfers(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// 清理测试数据
	_, _ = db.ExecContext(ctx, "DELETE FROM transfers WHERE block_number = 888888")
	_, _ = db.ExecContext(ctx, "DELETE FROM blocks WHERE number = 888888")

	// 开始事务
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// 插入测试区块
	blockNumber := big.NewInt(888888)
	blockHash := common.HexToHash("0xabcdef1234567890")

	_, err = tx.ExecContext(ctx,
		`INSERT INTO blocks (hash, number, timestamp, gas_limit, gas_used, transaction_count)
		 VALUES ($1, $2, NOW(), $3, $4, $5)`,
		blockHash.Hex(), blockNumber.String(), int64(21000), int64(21000), 3,
	)
	require.NoError(t, err)

	// 批量插入转账
	for i := 0; i < 3; i++ {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO transfers (transaction_hash, block_number, log_index, token_address, from_address, to_address, value)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			common.HexToHash("0x"+string(rune(i))).Hex(),
			blockNumber.String(),
			i,
			common.HexToAddress("0x1111").Hex(),
			common.HexToAddress("0x2222").Hex(),
			common.HexToAddress("0x3333").Hex(),
			"1000000000000000000",
		)
		require.NoError(t, err)
	}

	// 提交事务
	err = tx.Commit()
	require.NoError(t, err)

	// 验证数据
	var transferCount int
	err = db.GetContext(ctx, &transferCount,
		"SELECT COUNT(*) FROM transfers WHERE block_number = $1",
		blockNumber.String(),
	)
	require.NoError(t, err)
	assert.Equal(t, 3, transferCount, "should have 3 transfers")

	t.Logf("✅ Batch insert verified: %d transfers persisted", transferCount)
}

// TestBlockDataIntegrity 测试区块数据完整性
func TestBlockDataIntegrity(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sqlx.Connect("pgx", dbURL)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// 清理测试数据
	_, _ = db.ExecContext(ctx, "DELETE FROM blocks WHERE number = 777777")

	// 插入测试区块
	blockNumber := big.NewInt(777777)
	blockHash := common.HexToHash("0x1111111111111111")
	gasLimit := int64(30000000)
	gasUsed := int64(15000000)
	txCount := 100

	_, err = db.ExecContext(ctx,
		`INSERT INTO blocks (hash, number, timestamp, gas_limit, gas_used, transaction_count)
		 VALUES ($1, $2, NOW(), $3, $4, $5)`,
		blockHash.Hex(), blockNumber.String(), gasLimit, gasUsed, txCount,
	)
	require.NoError(t, err)

	// 读取并验证
	var hash, number string
	var readGasLimit, readGasUsed int64
	var readTxCount int

	err = db.QueryRowContext(ctx,
		`SELECT hash, number, gas_limit, gas_used, transaction_count FROM blocks WHERE number = $1`,
		blockNumber.String(),
	).Scan(&hash, &number, &readGasLimit, &readGasUsed, &readTxCount)

	require.NoError(t, err)
	assert.Equal(t, blockHash.Hex(), hash)
	assert.Equal(t, blockNumber.String(), number)
	assert.Equal(t, gasLimit, readGasLimit)
	assert.Equal(t, gasUsed, readGasUsed)
	assert.Equal(t, txCount, readTxCount)

	t.Logf("✅ Block data integrity verified: all fields match")
}
