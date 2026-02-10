package engine

import (
	"context"
	"database/sql"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ethereum/go-ethereum"
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

func (m *MockProcessorRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	block, exists := m.blocks[number.String()]
	if !exists {
		return nil, sql.ErrNoRows
	}
	return block.Header(), nil
}

func (m *MockProcessorRPCClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	return []types.Log{}, nil
}

func (m *MockProcessorRPCClient) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	return big.NewInt(100), nil
}

func (m *MockProcessorRPCClient) GetHealthyNodeCount() int { return 1 }
func (m *MockProcessorRPCClient) GetTotalNodeCount() int   { return 1 }
func (m *MockProcessorRPCClient) Close()                   {}

func (m *MockProcessorRPCClient) AddBlock(block *types.Block) {
	m.blocks[block.Number().String()] = block
}

func createTestBlock(number int64, hash string, parentHash string) *types.Block {
	header := &types.Header{
		Number:     big.NewInt(number),
		ParentHash: common.HexToHash(parentHash),
		Time:       uint64(time.Now().Unix()),
	}
	return types.NewBlockWithHeader(header)
}

func TestProcessor_NewProcessor(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	_ = mock // 显式忽略未使用的 mock 对象

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC, 500, 1)

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

	processor := NewProcessor(sqlxDB, mockRPC, 500, 1)

	// Setup mock expectations
	mock.ExpectBegin()

	// Mock parent block query
	mock.ExpectQuery("SELECT .* FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnError(sql.ErrNoRows)

	// Mock block insert (updated to 8 columns, using regexp for flexibility)
	mock.ExpectExec("INSERT INTO blocks").
		WithArgs("100", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Note: checkpoint is batched (every 100 blocks), so no checkpoint exec expected for a single block

	mock.ExpectCommit()

	// Create test block with valid hex hash
	block := createTestBlock(100, "0x"+strings.Repeat("a", 64), "0x"+strings.Repeat("b", 64))
	data := BlockData{Block: block, Number: big.NewInt(100), Logs: []types.Log{}}

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

	processor := NewProcessor(sqlxDB, mockRPC, 500, 1)

	mock.ExpectBegin()

	rows := sqlmock.NewRows([]string{"number", "hash", "parent_hash", "timestamp"}).
		AddRow("99", "0xold", "0x98", 1234567890)
	mock.ExpectQuery("SELECT .* FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnRows(rows)

	mock.ExpectRollback()

	// Parent hash mismatch: block expects 0xnew, DB has 0xold
	block := createTestBlock(100, "0xabc", "0xnew")
	data := BlockData{Block: block, Number: big.NewInt(100), Logs: []types.Log{}}

	ctx := context.Background()
	err = processor.ProcessBlock(ctx, data)

	assert.Error(t, err)
	assert.IsType(t, ReorgError{}, err)
}

func TestProcessor_UpdateCheckpoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	processor := NewProcessor(sqlxDB, nil, 500, 1)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO sync_checkpoints").
		WithArgs(1, "100").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	ctx := context.Background()
	err = processor.UpdateCheckpoint(ctx, 1, big.NewInt(100))

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcessor_ExtractTransfer(t *testing.T) {
	processor := NewProcessor(nil, nil, 500, 1)

	fromAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	toAddr := common.HexToAddress("0x9876543210987654321098765432109876543210")
	amount := big.NewInt(1000)
	txHash := common.HexToHash("0xabcdef")

	log := types.Log{
		Address: common.HexToAddress("0xabcd"),
		Topics: []common.Hash{
			TransferEventHash,
			common.BytesToHash(fromAddr.Bytes()),
			common.BytesToHash(toAddr.Bytes()),
		},
		Data:        common.LeftPadBytes(amount.Bytes(), 32),
		BlockNumber: 100,
		TxHash:      txHash,
		Index:       1,
	}

	transfer := processor.ExtractTransfer(log)

	assert.NotNil(t, transfer)
	assert.Equal(t, txHash.Hex(), transfer.TxHash)
	assert.Equal(t, amount.String(), transfer.Amount.String())
}

func TestProcessor_ExtractTransfer_InvalidEvent(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := NewMockProcessorRPCClient()

	processor := NewProcessor(sqlxDB, mockRPC, 500, 1)

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

	processor := NewProcessor(sqlxDB, mockRPC, 500, 1)

	// Setup RPC blocks (Ancestor at 99)
	block100 := createTestBlock(100, "0x100", "0x99")
	block99 := createTestBlock(99, "0x99", "0x98")
	block98 := createTestBlock(98, "0x98", "0x97")
	mockRPC.AddBlock(block100)
	mockRPC.AddBlock(block99)
	mockRPC.AddBlock(block98)

	// --- Step 1: Block 100 ---
	// DB check for block 100
	mock.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
		WithArgs("100").
		WillReturnError(sql.ErrNoRows) // Local block 100 missing

	// --- Step 2: Block 99 ---
	// DB check for block 99
	// Use the actual hash from the Geth block object to ensure matching
	rows := sqlmock.NewRows([]string{"hash"}).AddRow(block99.Hash().Hex())
	mock.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnRows(rows)

	ctx := context.Background()
	ancestorNum, ancestorHash, toDelete, err := processor.FindCommonAncestor(ctx, big.NewInt(100))

	assert.NoError(t, err)
	assert.Equal(t, "99", ancestorNum.String())
	assert.Equal(t, block99.Hash().Hex(), ancestorHash)
	assert.Len(t, toDelete, 1) // Block 100 was missing/mismatched, so added to delete list
	assert.Equal(t, "100", toDelete[0].String())
}
