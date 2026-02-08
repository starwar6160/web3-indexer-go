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

// MockProcessorRPCClient for testing processor
type MockProcessorRPCClient struct {
	blocks map[string]*types.Block
}

func NewMockProcessorRPCClient() *MockProcessorRPCClient {
	return &MockProcessorRPCClient{
		blocks: make(map[string]*types.Block),
	}
}

func (m *MockProcessorRPCClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	block, exists := m.blocks[number.String()]
	if !exists {
		return nil, sql.ErrNoRows
	}
	return block, nil
}

func (m *MockProcessorRPCClient) AddBlock(block *types.Block) {
	m.blocks[block.Number().String()] = block
}

func createProcessorTestBlock(number int64, hash string, parentHash string) *types.Block {
	header := &types.Header{
		Number:    big.NewInt(number),
		Hash:      common.HexToHash(hash),
		ParentHash: common.HexToHash(parentHash),
		Time:      uint64(time.Now().Unix()),
	}
	return types.NewBlockWithHeader(header)
}

func TestProcessor_NewProcessor(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	assert.NotNil(t, processor)
	assert.Equal(t, sqlxDB, processor.db)
	assert.Equal(t, mockRPC, processor.client)
}

func TestProcessor_ProcessBlock_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Setup mock expectations
	mock.ExpectBegin()
	
	// Mock parent block query (no rows for first block)
	mock.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnError(sql.ErrNoRows)
	
	// Mock block insert
	mock.ExpectExec("INSERT INTO blocks").
		WithArgs("100", "0x123", "0xabc", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	
	mock.ExpectCommit()

	// Create test block
	block := createProcessorTestBlock(100, "0x123", "0xabc")
	data := BlockData{Block: block, Logs: []types.Log{}}

	ctx := context.Background()
	err = processor.ProcessBlock(ctx, data)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ProcessBlock_ReorgDetected(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Setup mock expectations
	mock.ExpectBegin()
	
	// Mock parent block query with different hash (reorg)
	rows := sqlmock.NewRows([]string{"number", "hash", "parent_hash", "timestamp"}).
		AddRow("99", "0xoldhash", "0x98hash", 1234567890)
	mock.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnRows(rows)
	
	// Mock rollback (delete blocks >= 99)
	mock.ExpectExec("DELETE FROM blocks WHERE number >= \\$1").
		WithArgs("99").
		WillReturnResult(sqlmock.NewResult(1, 1))
	
	mock.ExpectRollback()

	// Create test block with different parent hash
	block := createTestBlock(100, "0x123", "0xnewhash") // Different from expected 0xoldhash
	data := BlockData{Block: block, Logs: []types.Log{}}

	ctx := context.Background()
	err = processor.ProcessBlock(ctx, data)

	assert.Error(t, err)
	assert.Equal(t, ErrReorgDetected, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ProcessBlock_FetchError(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Create test data with error
	data := BlockData{Err: assert.AnError}

	ctx := context.Background()
	err = processor.ProcessBlock(ctx, data)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetch error")
}

func TestProcessor_ProcessBlockWithRetry_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Setup mock expectations for successful retry
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO blocks").
		WithArgs("100", "0x123", "0xabc", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	block := createTestBlock(100, "0x123", "0xabc")
	data := BlockData{Block: block, Logs: []types.Log{}}

	ctx := context.Background()
	err = processor.ProcessBlockWithRetry(ctx, data, 3)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ProcessBlockWithRetry_FatalError(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Create test data with fatal error
	data := BlockData{Err: assert.AnError}

	ctx := context.Background()
	err = processor.ProcessBlockWithRetry(ctx, data, 3)

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err) // Should not retry fatal errors
}

func TestProcessor_UpdateCheckpoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Setup mock expectations
	mock.ExpectExec("INSERT INTO sync_checkpoints").
		WithArgs(1, "100").
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	err = processor.UpdateCheckpoint(ctx, 1, big.NewInt(100))

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ExtractTransfer(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Create a valid Transfer event log
	fromAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	toAddr := common.HexToAddress("0x9876543210987654321098765432109876543210")
	amount := big.NewInt(1000)

	log := types.Log{
		Address: common.HexToAddress("0xabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd"),
		Topics: []common.Hash{
			TransferEventHash, // Transfer event signature
			fromAddr.Hash(),
			toAddr.Hash(),
		},
		Data: common.LeftPadBytes(amount.Bytes(), 32),
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabcdef"),
		Index:       1,
	}

	transfer := processor.ExtractTransfer(log)

	assert.NotNil(t, transfer)
	assert.Equal(t, "100", transfer.BlockNumber.String())
	assert.Equal(t, "0xabcdef", transfer.TxHash)
	assert.Equal(t, uint(1), transfer.LogIndex)
	assert.Equal(t, fromAddr.Hex(), transfer.From)
	assert.Equal(t, toAddr.Hex(), transfer.To)
	assert.Equal(t, amount.String(), transfer.Amount.String())
}

func TestProcessor_ExtractTransfer_InvalidEvent(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Create an invalid log (wrong event signature)
	log := types.Log{
		Topics: []common.Hash{
			common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234"), // Wrong signature
		},
	}

	transfer := processor.ExtractTransfer(log)

	assert.Nil(t, transfer)
}

func TestProcessor_FindCommonAncestor(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC)

	// Setup RPC blocks
	block100 := createProcessorTestBlock(100, "0x100", "0x99")
	block99 := createProcessorTestBlock(99, "0x99", "0x98")
	block98 := createProcessorTestBlock(98, "0x98", "0x97")

	mockRPC.AddBlock(block100)
	mockRPC.AddBlock(block99)
	mockRPC.AddBlock(block98)

	// Setup database responses
	rows := sqlmock.NewRows([]string{"hash"}).
		AddRow("0x99") // Matching hash at block 99
	mock.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
		WithArgs("100").
		WillReturnError(sql.ErrNoRows) // No local block 100

	mock.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnRows(rows)

	ctx := context.Background()
	ancestorNum, ancestorHash, toDelete, err := processor.FindCommonAncestor(ctx, big.NewInt(100))

	assert.NoError(t, err)
	assert.Equal(t, "99", ancestorNum.String())
	assert.Equal(t, "0x99", ancestorHash)
	assert.Len(t, toDelete, 1) // Block 100 should be deleted
	assert.Equal(t, "100", toDelete[0].String())
}
