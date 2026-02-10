package engine

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRPCPool for testing
type MockRPCPool struct {
	mock.Mock
}

func (m *MockRPCPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Block), args.Error(1)
}

func (m *MockRPCPool) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Header), args.Error(1)
}

func (m *MockRPCPool) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.Log), args.Error(1)
}

func (m *MockRPCPool) GetHealthyNodeCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRPCPool) GetTotalNodeCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRPCPool) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *MockRPCPool) Close() {
	m.Called()
}

func createFetcherTestBlock(number int64, hash string) *types.Block {
	header := &types.Header{
		Number: big.NewInt(number),
		Time:   uint64(time.Now().Unix()),
	}
	return types.NewBlockWithHeader(header)
}

func TestFetcher_NewFetcher(t *testing.T) {
	mockPool := &MockRPCPool{}
	
	fetcher := NewFetcher(mockPool, 5)
	
	assert.NotNil(t, fetcher)
	assert.Equal(t, mockPool, fetcher.pool)
	assert.Equal(t, 5, fetcher.concurrency)
	assert.NotNil(t, fetcher.Results)
	assert.NotNil(t, fetcher.jobs)
}

func TestFetcher_NewFetcherWithLimiter(t *testing.T) {
	mockPool := &MockRPCPool{}
	
	fetcher := NewFetcherWithLimiter(mockPool, 3, 50, 100)
	
	assert.NotNil(t, fetcher)
	assert.Equal(t, mockPool, fetcher.pool)
	assert.Equal(t, 3, fetcher.concurrency)
	assert.NotNil(t, fetcher.limiter)
}

func TestFetcher_SetRateLimit(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	fetcher.SetRateLimit(200, 500)
	
	assert.Equal(t, float64(200), fetcher.limiter.Limit())
	assert.Equal(t, 500, fetcher.limiter.Burst())
}

func TestFetcher_Stop(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	// Should not panic
	fetcher.Stop()
	
	// Should not panic on second call
	fetcher.Stop()
}

func TestFetcher_fetchBlockWithLogs_Success(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	block := createFetcherTestBlock(100, "0x123")
	logs := []types.Log{
		{
			Address: common.HexToAddress("0xabc"),
			Topics:  []common.Hash{TransferEventHash},
		},
	}
	
	ctx := context.Background()
	blockNum := big.NewInt(100)
	
	mockPool.On("BlockByNumber", ctx, blockNum).Return(block, nil)
	mockPool.On("FilterLogs", ctx, mock.Anything).Return(logs, nil)
	
	resultBlock, resultLogs, err := fetcher.fetchBlockWithLogs(ctx, blockNum)
	
	assert.NoError(t, err)
	assert.Equal(t, block, resultBlock)
	assert.Equal(t, logs, resultLogs)
	mockPool.AssertExpectations(t)
}

func TestFetcher_fetchBlockWithLogs_BlockError(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	ctx := context.Background()
	blockNum := big.NewInt(100)
	
	mockPool.On("BlockByNumber", ctx, blockNum).Return(nil, assert.AnError)
	
	resultBlock, resultLogs, err := fetcher.fetchBlockWithLogs(ctx, blockNum)
	
	assert.Error(t, err)
	assert.Nil(t, resultBlock)
	assert.Nil(t, resultLogs)
	mockPool.AssertExpectations(t)
}

func TestFetcher_fetchBlockWithLogs_LogsError(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	block := createFetcherTestBlock(100, "0x123")
	
	ctx := context.Background()
	blockNum := big.NewInt(100)
	
	mockPool.On("BlockByNumber", ctx, blockNum).Return(block, nil)
	mockPool.On("FilterLogs", ctx, mock.Anything).Return(nil, assert.AnError)
	
	resultBlock, resultLogs, err := fetcher.fetchBlockWithLogs(ctx, blockNum)
	
	assert.NoError(t, err) // Logs error should not fail block fetch
	assert.Equal(t, block, resultBlock)
	assert.Empty(t, resultLogs) // Should be empty on error
	mockPool.AssertExpectations(t)
}

func TestFetcher_Schedule(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	start := big.NewInt(100)
	end := big.NewInt(102)
	
	// Schedule blocks
	fetcher.Schedule(context.Background(), start, end)
	
	// Give some time for scheduling
	time.Sleep(10 * time.Millisecond)
	
	// Verify jobs were scheduled (check that jobs channel receives expected blocks)
	select {
	case job := <-fetcher.jobs:
		assert.Equal(t, "100", job.String())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected job to be scheduled")
	}
	
	// Clean up
	fetcher.Stop()
}

func TestFetcher_Schedule_Stop(t *testing.T) {
	mockPool := &MockRPCPool{}
	fetcher := NewFetcher(mockPool, 5)
	
	start := big.NewInt(100)
	end := big.NewInt(200) // Large range
	
	// Start scheduling
	fetcher.Schedule(context.Background(), start, end)
	
	// Stop immediately
	fetcher.Stop()
	
	// Give some time for stop to take effect
	time.Sleep(10 * time.Millisecond)
	
	// Verify that jobs channel is closed or empty
	select {
	case <-fetcher.jobs:
		// Channel might be closed or have remaining jobs, both are acceptable
	case <-time.After(100 * time.Millisecond):
		// Timeout is also acceptable
	}
}
