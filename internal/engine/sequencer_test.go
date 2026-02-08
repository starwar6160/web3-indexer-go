package engine

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	assert.NotNil(t, sequencer)
	assert.Equal(t, startBlock.String(), sequencer.GetExpectedBlock().String())
	assert.Equal(t, 0, sequencer.GetBufferSize())
}

func TestSequencer_HandleBlock_ExpectedBlock(t *testing.T) {
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	// Create test block data
	block := &types.Block{}
	blockNum := big.NewInt(100)
	data := BlockData{Block: block}

	// Mock successful processing
	mockProcessor.On("ProcessBlockWithRetry", mock.Anything, data, 3).Return(nil)

	ctx := context.Background()
	err := sequencer.handleBlock(ctx, data)

	assert.NoError(t, err)
	assert.Equal(t, "101", sequencer.GetExpectedBlock().String()) // Should increment
	mockProcessor.AssertExpectations(t)
}

func TestSequencer_HandleBlock_OutOfOrderBlock(t *testing.T) {
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	// Create test block data with higher number
	block := &types.Block{}
	blockNum := big.NewInt(102) // Out of order
	data := BlockData{Block: block}

	ctx := context.Background()
	err := sequencer.handleBlock(ctx, data)

	assert.NoError(t, err)
	assert.Equal(t, "100", sequencer.GetExpectedBlock().String()) // Should not change
	assert.Equal(t, 1, sequencer.GetBufferSize())               // Should be buffered
}

func TestSequencer_ProcessBufferContinuations(t *testing.T) {
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	// Add blocks to buffer
	block101 := &types.Block{}
	block102 := &types.Block{}
	
	sequencer.buffer["101"] = BlockData{Block: block101}
	sequencer.buffer["102"] = BlockData{Block: block102}

	// Mock successful processing
	mockProcessor.On("ProcessBlockWithRetry", mock.Anything, BlockData{Block: block101}, 3).Return(nil)
	mockProcessor.On("ProcessBlockWithRetry", mock.Anything, BlockData{Block: block102}, 3).Return(nil)

	ctx := context.Background()
	sequencer.processBufferContinuations(ctx)

	assert.Equal(t, "102", sequencer.GetExpectedBlock().String())
	assert.Equal(t, 0, sequencer.GetBufferSize())
	mockProcessor.AssertExpectations(t)
}

func TestSequencer_HandleReorg(t *testing.T) {
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	// Add blocks to buffer that should be cleared
	sequencer.buffer["100"] = BlockData{}
	sequencer.buffer["101"] = BlockData{}
	sequencer.buffer["102"] = BlockData{}

	block := &types.Block{}
	blockNum := big.NewInt(100)
	data := BlockData{Block: block}

	ctx := context.Background()
	err := sequencer.handleReorg(ctx, data)

	assert.Error(t, err)
	assert.Equal(t, ErrReorgNeedRefetch, err)
	assert.Equal(t, "100", sequencer.GetExpectedBlock().String())
	assert.Equal(t, 0, sequencer.GetBufferSize()) // Should be cleared
}

func TestSequencer_DrainBuffer(t *testing.T) {
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	// Add expected block to buffer
	block := &types.Block{}
	sequencer.buffer["100"] = BlockData{Block: block}

	// Mock successful processing
	mockProcessor.On("ProcessBlockWithRetry", mock.Anything, BlockData{Block: block}, 3).Return(nil)

	ctx := context.Background()
	sequencer.drainBuffer(ctx)

	assert.Equal(t, "101", sequencer.GetExpectedBlock().String())
	assert.Equal(t, 0, sequencer.GetBufferSize())
	mockProcessor.AssertExpectations(t)
}

func TestSequencer_BufferOverflow(t *testing.T) {
	mockProcessor := &MockProcessor{}
	startBlock := big.NewInt(100)
	chainID := int64(1)
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	sequencer := NewSequencer(mockProcessor, startBlock, chainID, resultCh, fatalErrCh)

	// Fill buffer to overflow
	for i := 0; i < 1001; i++ {
		sequencer.buffer[string(rune(100+i))] = BlockData{}
	}

	block := &types.Block{}
	blockNum := big.NewInt(2000) // Far ahead
	data := BlockData{Block: block}

	ctx := context.Background()
	err := sequencer.handleBlock(ctx, data)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffer overflow")
}
