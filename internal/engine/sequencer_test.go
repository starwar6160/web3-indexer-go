package engine

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type SequencerMockRPCClient struct {
	mock.Mock
}

func (m *SequencerMockRPCClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Block), args.Error(1)
}

func (m *SequencerMockRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Header), args.Error(1)
}

func (m *SequencerMockRPCClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, q)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.Log), args.Error(1)
}

func (m *SequencerMockRPCClient) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *SequencerMockRPCClient) GetHealthyNodeCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *SequencerMockRPCClient) GetTotalNodeCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *SequencerMockRPCClient) Close() {
	m.Called()
}

func TestSequencer_NewSequencer(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := new(SequencerMockRPCClient)

	processor := NewProcessor(sqlxDB, mockRPC, 100, 1)
	fetcher := NewFetcher(mockRPC, 1)

	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 10)

	sequencer := NewSequencerWithFetcher(processor, fetcher, big.NewInt(100), 1, resultCh, fatalErrCh, nil, nil)

	assert.NotNil(t, sequencer)
	assert.Equal(t, processor, sequencer.processor)
	assert.Equal(t, fetcher, sequencer.fetcher)
	assert.Equal(t, int64(1), sequencer.chainID)
}

func TestSequencer_Run_ProcessSequential(t *testing.T) {
	db, mockDB, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := new(SequencerMockRPCClient)

	parentHash := common.HexToHash("0x1234")
	header := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: parentHash,
		Time:       1234567890,
	}
	block := types.NewBlockWithHeader(header)

	mockDB.ExpectBegin()
	// Parent check for block 100 looks for block 99
	mockDB.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnRows(sqlmock.NewRows([]string{"number", "hash", "parent_hash", "timestamp"}).
			AddRow("99", parentHash.Hex(), "0x0000", 1234567880))

	mockDB.ExpectExec("INSERT INTO blocks").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mockDB.ExpectCommit()

	processor := NewProcessor(sqlxDB, mockRPC, 100, 1)
	processor.SetBatchCheckpoint(1000) // Avoid checkpoint update in this test
	fetcher := NewFetcher(mockRPC, 1)

	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 10)

	sequencer := NewSequencerWithFetcher(processor, fetcher, big.NewInt(100), 1, resultCh, fatalErrCh, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		sequencer.Run(ctx)
	}()

	resultCh <- BlockData{
		Block:  block,
		Number: big.NewInt(100),
	}

	time.Sleep(200 * time.Millisecond)

	assert.NoError(t, mockDB.ExpectationsWereMet())

	cancel()
	wg.Wait()
}

func TestSequencer_Run_BufferOutOfOrder(t *testing.T) {
	db, mockDB, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	mockRPC := new(SequencerMockRPCClient)

	parentHash100 := common.HexToHash("0x0099")
	header100 := &types.Header{Number: big.NewInt(100), ParentHash: parentHash100, Time: 1234567890}
	block100 := types.NewBlockWithHeader(header100)

	header101 := &types.Header{Number: big.NewInt(101), ParentHash: block100.Hash(), Time: 1234567900}
	block101 := types.NewBlockWithHeader(header101)

	// Block 100 Expectations
	mockDB.ExpectBegin()
	mockDB.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("99").
		WillReturnRows(sqlmock.NewRows([]string{"number", "hash", "parent_hash", "timestamp"}).
			AddRow("99", parentHash100.Hex(), "0x0098", 1234567880))
	mockDB.ExpectExec("INSERT INTO blocks").WillReturnResult(sqlmock.NewResult(1, 1))
	mockDB.ExpectCommit()

	// Block 101 Expectations
	mockDB.ExpectBegin()
	mockDB.ExpectQuery("SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = \\$1").
		WithArgs("100").
		WillReturnRows(sqlmock.NewRows([]string{"number", "hash", "parent_hash", "timestamp"}).
			AddRow("100", block100.Hash().Hex(), parentHash100.Hex(), 1234567890))
	mockDB.ExpectExec("INSERT INTO blocks").WillReturnResult(sqlmock.NewResult(1, 1))
	mockDB.ExpectCommit()

	processor := NewProcessor(sqlxDB, mockRPC, 100, 1)
	processor.SetBatchCheckpoint(1000)
	fetcher := NewFetcher(mockRPC, 1)

	resultCh := make(chan BlockData, 10)
	fatalErrCh := make(chan error, 10)

	sequencer := NewSequencerWithFetcher(processor, fetcher, big.NewInt(100), 1, resultCh, fatalErrCh, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		sequencer.Run(ctx)
	}()

	// Send 101 first (Buffer)
	resultCh <- BlockData{Block: block101, Number: big.NewInt(101)}
	time.Sleep(50 * time.Millisecond)

	// Send 100 (Trigger 100 and then 101)
	resultCh <- BlockData{Block: block100, Number: big.NewInt(100)}
	time.Sleep(200 * time.Millisecond)

	assert.NoError(t, mockDB.ExpectationsWereMet())

	cancel()
	wg.Wait()
}