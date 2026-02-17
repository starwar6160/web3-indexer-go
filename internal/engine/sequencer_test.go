package engine

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

// MockProcessor implements BlockProcessor for testing
type MockProcessor struct{}

func (m *MockProcessor) ProcessBlockWithRetry(_ context.Context, _ BlockData, _ int) error {
	return nil
}

func (m *MockProcessor) ProcessBatch(_ context.Context, _ []BlockData, _ int64) error {
	return nil
}

func (m *MockProcessor) GetRPCClient() RPCClient {
	return nil
}

func TestSequencer_Reordering(t *testing.T) {
	// 1. Setup mock sequencer with MockProcessor
	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 1)

	startBlock := big.NewInt(100)
	mockProc := &MockProcessor{}
	seq := NewSequencer(mockProc, startBlock, 1, resultCh, fatalErrCh, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Helper to create valid BlockData
	makeBD := func(n int64) BlockData {
		return BlockData{
			Number: big.NewInt(n),
			Block:  types.NewBlockWithHeader(&types.Header{Number: big.NewInt(n)}),
		}
	}

	// 2. Test the reordering logic

	// Simulate 100 arriving - should process immediately
	bd100 := makeBD(100)
	err := seq.handleBatch(ctx, []BlockData{bd100})
	assert.NoError(t, err)
	assert.Equal(t, "101", seq.expectedBlock.String(), "Expected block should advance to 101")
	assert.Empty(t, seq.buffer, "Buffer should be empty")

	// Simulate 102 arriving - should be buffered
	bd102 := makeBD(102)
	err = seq.handleBatch(ctx, []BlockData{bd102})
	assert.NoError(t, err)
	assert.Equal(t, "101", seq.expectedBlock.String(), "Expected block should still be 101")
	assert.Contains(t, seq.buffer, "102", "102 should be in buffer")

	// Simulate 101 arriving - should trigger 101 and 102
	bd101 := makeBD(101)
	err = seq.handleBatch(ctx, []BlockData{bd101})
	assert.NoError(t, err)
	assert.Equal(t, "103", seq.expectedBlock.String(), "Expected block should advance to 103 after 101 and 102")
	assert.Empty(t, seq.buffer, "Buffer should be empty after processing 101 and 102")
}

func TestSequencer_GapFillTrigger(_ *testing.T) {
	// Test the logic that detects a stall and triggers a gap fill
	resultCh := make(chan BlockData)
	fatalErrCh := make(chan error)
	startBlock := big.NewInt(100)

	mockProc := &MockProcessor{}
	seq := NewSequencer(mockProc, startBlock, 1, resultCh, fatalErrCh, nil)

	// Mock 102 in buffer, but we are waiting for 100
	seq.mu.Lock()
	seq.buffer["102"] = BlockData{
		Number: big.NewInt(102),
		Block:  types.NewBlockWithHeader(&types.Header{Number: big.NewInt(102)}),
	}
	seq.expectedBlock = big.NewInt(100)
	seq.lastProgressAt = time.Now().Add(-60 * time.Second) // Force idle timeout
	seq.mu.Unlock()

	// Just verify handleStall doesn't panic
	seq.handleStall(context.Background())
}
