package engine

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRPCClient is a mock implementation of the RPCClient interface
type MockRPCClient struct {
	mock.Mock
}

func (m *MockRPCClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	block, _ := args.Get(0).(*types.Block)
	return block, args.Error(1)
}

func (m *MockRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	header, _ := args.Get(0).(*types.Header)
	return header, args.Error(1)
}

func (m *MockRPCClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, q)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	logs, _ := args.Get(0).([]types.Log)
	return logs, args.Error(1)
}

func (m *MockRPCClient) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *MockRPCClient) GetHealthyNodeCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRPCClient) GetTotalNodeCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRPCClient) Close() {
	m.Called()
}

func TestFetcher_NewFetcher(t *testing.T) {
	rpcClient := &MockRPCClient{}
	fetcher := NewFetcher(rpcClient, 5)

	assert.NotNil(t, fetcher)
	assert.Equal(t, 5, fetcher.concurrency)
	assert.NotNil(t, fetcher.jobs)
	assert.NotNil(t, fetcher.Results)
}

func TestFetcher_fetchBlockWithLogs_Success(t *testing.T) {
	mockRPC := new(MockRPCClient)

	// Create a mock block
	header := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.HexToHash("0x1234"),
		Time:       1234567890,
		GasLimit:   30000000,
		GasUsed:    8421505,
		BaseFee:    big.NewInt(1000000000),
	}
	block := types.NewBlockWithHeader(header)

	// Set up the mock expectation
	mockRPC.On("BlockByNumber", mock.Anything, big.NewInt(100)).Return(block, nil)
	// Add FilterLogs expectation
	mockRPC.On("FilterLogs", mock.Anything, mock.AnythingOfType("ethereum.FilterQuery")).Return([]types.Log{}, nil)

	fetcher := NewFetcher(mockRPC, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	blockResult, logs, err := fetcher.fetchBlockWithLogs(ctx, big.NewInt(100))

	assert.NotNil(t, blockResult)
	assert.Equal(t, big.NewInt(100), blockResult.Number())
	assert.NotNil(t, logs)
	assert.NoError(t, err)
	mockRPC.AssertExpectations(t)
}

func TestFetcher_fetchBlockWithLogs_Error(t *testing.T) {
	mockRPC := new(MockRPCClient)

	// Set up the mock expectation to return an error
	mockRPC.On("BlockByNumber", mock.Anything, big.NewInt(100)).Return((*types.Block)(nil), errors.New("connection failed"))

	fetcher := NewFetcher(mockRPC, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	blockResult, logs, err := fetcher.fetchBlockWithLogs(ctx, big.NewInt(100))

	assert.Nil(t, blockResult)
	assert.Nil(t, logs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
	mockRPC.AssertExpectations(t)
}

func TestFetcher_StartStop(t *testing.T) {
	mockRPC := new(MockRPCClient)

	// Don't expect any calls since we're just testing start/stop
	fetcher := NewFetcher(mockRPC, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	// Start the fetcher
	fetcher.Start(ctx, &wg)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to stop
	cancel()

	// Wait for graceful shutdown
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Fetcher did not stop within timeout")
	}
}

func TestFetcher_Schedule(t *testing.T) {
	mockRPC := new(MockRPCClient)

	fetcher := NewFetcher(mockRPC, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start a goroutine to consume jobs so Schedule doesn't block
	jobsReceived := 0
	done := make(chan struct{})
	go func() {
		for range fetcher.jobs {
			jobsReceived++
			if jobsReceived == 6 {
				break
			}
		}
		close(done)
	}()

	// Schedule a range of blocks (100 to 105 is 6 blocks)
	err := fetcher.Schedule(ctx, big.NewInt(100), big.NewInt(105))

	assert.NoError(t, err)

	// Wait for consumer to finish
	select {
	case <-done:
		assert.Equal(t, 6, jobsReceived)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for jobs to be consumed")
	}
}

func TestFetcher_PauseResume(t *testing.T) {
	mockRPC := new(MockRPCClient)
	fetcher := NewFetcher(mockRPC, 1)

	assert.False(t, fetcher.IsPaused())

	fetcher.Pause()
	assert.True(t, fetcher.IsPaused())

	fetcher.Resume()
	assert.False(t, fetcher.IsPaused())
}

func TestFetcher_worker_Pause(t *testing.T) {
	mockRPC := new(MockRPCClient)
	fetcher := NewFetcher(mockRPC, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go fetcher.worker(ctx, &wg)

	// Pause the fetcher
	fetcher.Pause()

	// Send a job
	fetcher.jobs <- big.NewInt(100)

	// Wait a bit, it shouldn't be processed
	select {
	case <-fetcher.Results:
		t.Fatal("Job should not have been processed while paused")
	case <-time.After(200 * time.Millisecond):
		// Expected
	}

	// Mock expectations for when it resumes
	header := &types.Header{Number: big.NewInt(100)}
	block := types.NewBlockWithHeader(header)
	mockRPC.On("BlockByNumber", mock.Anything, big.NewInt(100)).Return(block, nil)
	mockRPC.On("FilterLogs", mock.Anything, mock.Anything).Return([]types.Log{}, nil)

	// Resume
	fetcher.Resume()

	// Now it should be processed
	select {
	case result := <-fetcher.Results:
		assert.Equal(t, big.NewInt(100), result.Number)
	case <-time.After(1 * time.Second):
		t.Fatal("Job should have been processed after resume")
	}

	cancel()
	wg.Wait()
}
