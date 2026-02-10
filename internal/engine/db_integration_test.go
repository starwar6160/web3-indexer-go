//go:build integration
package engine

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/holiman/uint256"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sqlx.DB {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable"
	}

	db, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err, "必须连接到测试数据库")

	// 强制应用最新 Schema 补丁（工业级防御）
	_, _ = db.Exec("ALTER TABLE blocks ADD COLUMN IF NOT EXISTS parent_hash VARCHAR(66) NOT NULL DEFAULT ''")
	_, _ = db.Exec("ALTER TABLE blocks ADD COLUMN IF NOT EXISTS gas_limit BIGINT NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE blocks ADD COLUMN IF NOT EXISTS gas_used BIGINT NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE blocks ADD COLUMN IF NOT EXISTS transaction_count INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE blocks ADD COLUMN IF NOT EXISTS base_fee_per_gas NUMERIC(78,0)")
	_, _ = db.Exec("ALTER TABLE transfers ADD COLUMN IF NOT EXISTS tx_hash CHAR(66) NOT NULL DEFAULT ''")

	// 测试隔离：清空表
	_, err = db.Exec("TRUNCATE blocks, transfers RESTART IDENTITY CASCADE")
	require.NoError(t, err)

	return db
}

// TestMirror_SchemaAlignment 验证 Go 结构体标签与数据库列名的物理对齐
func TestMirror_SchemaAlignment(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	inserter := NewBulkInserter(db)
	ctx := context.Background()

	// 1. 构造一个包含所有边缘字段的 Block
	mockBlock := models.Block{
		Number:           models.NewBigInt(888888),
		Hash:             "0x" + strings.Repeat("a", 64),
		ParentHash:       "0x" + strings.Repeat("0", 64),
		Timestamp:        uint64(time.Now().Unix()),
		GasLimit:         30000000,
		GasUsed:          15000000,
		TransactionCount: 10,
		BaseFeePerGas:    &models.BigInt{Int: big.NewInt(1000000000)},
	}

	// 2. 执行批量插入 (触发 COPY 协议或 UNNEST 逻辑)
	err := inserter.InsertBlocksBatch(ctx, []models.Block{mockBlock})
	
	// 断言：如果字段不匹配（如 tx_hash vs hash），这里会立即崩溃
	assert.NoError(t, err, "数据库 Schema 与 Go 模型必须物理对齐")

	// 3. 验证回读
	var saved models.Block
	err = db.Get(&saved, "SELECT * FROM blocks WHERE number = $1", "888888")
	assert.NoError(t, err)
	assert.Equal(t, mockBlock.Hash, saved.Hash)
}

// TestGauntlet_EVMPrecision 专项压测：256位极大值存储与回读
func TestGauntlet_EVMPrecision(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	inserter := NewBulkInserter(db)
	ctx := context.Background()

	// 1. 预插入父区块以满足外键约束
	db.Exec("INSERT INTO blocks (number, hash, parent_hash, timestamp, gas_limit, gas_used, transaction_count) VALUES (999, '0x999', '0x0', 123, 30000000, 1000000, 1)")

	// 2. 构造 MaxUint256
	maxVal := uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	
	transfer := models.Transfer{
		BlockNumber:  models.NewBigInt(999),
		TxHash:       "0x" + strings.Repeat("f", 64),
		LogIndex:     1,
		From:         "0x" + strings.Repeat("1", 40),
		To:           "0x" + strings.Repeat("2", 40),
		Amount:       models.Uint256{Int: maxVal},
		TokenAddress: "0x" + strings.Repeat("3", 40),
	}

	// 3. 尝试存储
	err := inserter.InsertTransfersBatch(ctx, []models.Transfer{transfer})
	assert.NoError(t, err, "NUMERIC(78,0) 必须能承载 MaxUint256")

	// 4. 极限回读校验：确保精度没有被 Postgres 截断
	var savedAmount string
	err = db.Get(&savedAmount, "SELECT amount::text FROM transfers WHERE tx_hash = $1", transfer.TxHash)
	assert.NoError(t, err)
	assert.Equal(t, maxVal.Dec(), savedAmount, "数据库中的十进制字符串必须与原始 MaxUint256 完全相等")
}

// TestSanity_NotNullConstraints 验证 NOT NULL 约束的拦截能力
func TestSanity_NotNullConstraints(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// 故意尝试插入缺少非空字段的数据 (通过原生 SQL 绕过 Go 层面的默认值赋值)
	_, err := db.Exec("INSERT INTO blocks (number, hash) VALUES (1, '0x1')")
	
	// 断言：由于我们设置了 NOT NULL，数据库必须报错，而不是存入脏数据
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "null value in column \"parent_hash\"")
}