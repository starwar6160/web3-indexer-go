package engine

import (
	"context"
	"database/sql"
	"math/big"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRPCClient for testing
type MockRPCClient struct {
	mock.Mock
}

func (m *MockRPCClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Block), args.Error(1)
}

// MockProcessor for testing
type MockProcessor struct {
	mock.Mock
}

func (m *MockProcessor) ProcessBlockWithRetry(ctx context.Context, data BlockData, maxRetries int) error {
	args := m.Called(ctx, data, maxRetries)
	return args.Error(0)
}

func (m *MockProcessor) UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error {
	args := m.Called(ctx, chainID, blockNumber)
	return args.Error(0)
}

func TestSequencer_NewSequencer(t *testing.T) {
	processor := &Processor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, nil)

	assert.NotNil(t, sequencer)
	assert.Equal(t, startBlock.String(), sequencer.GetExpectedBlock().String())
	assert.Equal(t, 0, sequencer.GetBufferSize())
}

func TestSequencer_HandleBlock_ExpectedBlock(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")

	processor := NewProcessor(sqlxDB, nil, 500, 1)
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, nil)

	// Create test block data
	block := createTestBlock(100, "0x100", "0x99")
	data := BlockData{Block: block, Number: big.NewInt(100)}

	// Mock DB expectation for ProcessBlock
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM blocks WHERE number = \\$1").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO blocks").WillReturnResult(sqlmock.NewResult(1, 1))
	// checkpoint is batched (every 100 blocks), no checkpoint exec for single block
	mock.ExpectCommit()

	ctx := context.Background()
	err := sequencer.handleBlock(ctx, data)

	assert.NoError(t, err)
	assert.Equal(t, "101", sequencer.GetExpectedBlock().String()) // Should increment
}

func TestSequencer_HandleBlock_OutOfOrderBlock(t *testing.T) {
	processor := &Processor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, nil)

	// Create test block data with higher number
	block := createTestBlock(102, "0x102", "0x101")
	data := BlockData{Block: block, Number: big.NewInt(102)}

	ctx := context.Background()
	err := sequencer.handleBlock(ctx, data)

	assert.NoError(t, err)
	assert.Equal(t, "100", sequencer.GetExpectedBlock().String()) // Should not change
	assert.Equal(t, 1, sequencer.GetBufferSize())                 // Should be buffered
}

func TestSequencer_BufferOverflow(t *testing.T) {
	processor := &Processor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(processor, startBlock, chainID, resultCh, fatalErrCh, nil)

	// Fill buffer to overflow (limit is 1000)
	for i := 0; i < 1001; i++ {
		num := big.NewInt(int64(200 + i))
		sequencer.buffer[num.String()] = BlockData{Number: num}
	}

	block := createTestBlock(2000, "0x2000", "0x1999")
	data := BlockData{Block: block, Number: big.NewInt(2000)}

	ctx := context.Background()
	err := sequencer.handleBlock(ctx, data)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffer overflow")
}
