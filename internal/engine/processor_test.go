package engine

import (
	"context"
	"database/sql"
	"math/big"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessor_NewProcessor(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	
	processor := NewProcessor(sqlxDB, nil, 500, 1)

	assert.NotNil(t, processor)
	assert.Equal(t, sqlxDB, processor.db)
	assert.Equal(t, 500, cap(processor.retryQueue))
	assert.Equal(t, int64(1), processor.chainID)
}

func TestProcessor_ProcessBlock_HappyPath(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	
	processor := NewProcessor(sqlxDB, nil, 500, 1)
	processor.SetBatchCheckpoint(1)

	// Create test block
	header := &types.Header{
		Number:     big.NewInt(43),
		ParentHash: common.HexToHash("0x4242424242424242424242424242424242424242424242424242424242424242"),
		Time:       1234567890,
		GasLimit:   30000000,
		GasUsed:    8421505,
		BaseFee:    big.NewInt(1000000000),
	}
	block := types.NewBlockWithHeader(header)

	// Setup mock expectations
	mock.ExpectBegin()

	// Mock parent block query (should return sql.ErrNoRows to simulate first block or fresh sync)
	mock.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("42").
		WillReturnError(sql.ErrNoRows)

	// Mock block insert
	mock.ExpectExec("INSERT INTO blocks").
		WithArgs(
			"43",                           // number
			block.Hash().Hex(),             // hash
			"0x4242424242424242424242424242424242424242424242424242424242424242", // parent_hash
			uint64(1234567890),             // timestamp
			uint64(30000000),              // gas_limit
			uint64(8421505),               // gas_used
			"1000000000",                  // base_fee_per_gas
			0,                             // transaction_count
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock checkpoint update
	mock.ExpectExec("INSERT INTO sync_checkpoints").
		WithArgs(int64(1), "43").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = processor.ProcessBlock(ctx, BlockData{
		Block:  block,
		Number: big.NewInt(43),
		Logs:   []types.Log{},
		Err:    nil,
	})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ProcessBlock_ReorgDetection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	
	processor := NewProcessor(sqlxDB, nil, 500, 1)

	// Setup mock expectations
	mock.ExpectBegin()

	// Mock parent block query - return a block with different parent hash
	rows := sqlmock.NewRows([]string{"number", "hash", "parent_hash", "timestamp"}).
		AddRow("42", "0xoldparent", "0xoldgrandparent", uint64(1234567889))
	mock.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("42").
		WillReturnRows(rows)

	mock.ExpectRollback()

	// Create test block with different parent hash
	header := &types.Header{
		Number:     big.NewInt(43),
		ParentHash: common.HexToHash("0x4242424242424242424242424242424242424242424242424242424242424242"), // Different from what's in DB
		Time:       1234567890,
		GasLimit:   30000000,
		GasUsed:    8421505,
		BaseFee:    big.NewInt(1000000000),
	}
	block := types.NewBlockWithHeader(header)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = processor.ProcessBlock(ctx, BlockData{
		Block:  block,
		Number: big.NewInt(43),
		Logs:   []types.Log{},
		Err:    nil,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reorg")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ExtractTransfer(t *testing.T) {
	processor := NewProcessor(nil, nil, 500, 1)

	// Create a mock Transfer event log
	log := types.Log{
		Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Topics: []common.Hash{
			TransferEventHash, // Transfer event signature
			common.HexToHash("0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), // from
			common.HexToHash("0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), // to
		},
		Data: common.Hex2Bytes("0000000000000000000000000000000000000000000000000de0b6b3a7640000"), // 1 ETH in hex
	}

	transfer := processor.ExtractTransfer(log)

	require.NotNil(t, transfer)
	assert.Equal(t, "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", transfer.From)
	assert.Equal(t, "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", transfer.To)
	assert.Equal(t, "1000000000000000000", transfer.Amount.String()) // 1 ETH
	assert.Equal(t, "0x1111111111111111111111111111111111111111", transfer.TokenAddress)
}

func TestProcessor_SetWatchedAddresses(t *testing.T) {
	processor := NewProcessor(nil, nil, 500, 1)

	addresses := []string{
		"0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222",
	}

	processor.SetWatchedAddresses(addresses)

	assert.Equal(t, 2, len(processor.watchedAddresses))
	assert.True(t, processor.watchedAddresses[common.HexToAddress(addresses[0])])
	assert.True(t, processor.watchedAddresses[common.HexToAddress(addresses[1])])
}