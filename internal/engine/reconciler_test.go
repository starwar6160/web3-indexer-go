package engine

import (
	"context"
	"math/big"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestReconciler_auditBlock(t *testing.T) {
	db, mockDB, _ := sqlmock.New()
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")

	mockRPC := new(MockRPCClient)
	reconciler := NewReconciler(sqlxDB, mockRPC, GetMetrics())

	ctx := context.Background()
	blockNum := big.NewInt(100)

	t.Run("AuditPassed", func(t *testing.T) {
		header := &types.Header{Number: blockNum}
		block := types.NewBlockWithHeader(header)
		mockRPC.On("BlockByNumber", mock.Anything, blockNum).Return(block, nil).Once()

		rows := sqlmock.NewRows([]string{"hash"}).AddRow(block.Hash().Hex())
		mockDB.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
			WithArgs(blockNum.String()).
			WillReturnRows(rows)

		reconciler.auditBlock(ctx, blockNum)
		assert.NoError(t, mockDB.ExpectationsWereMet())
	})

	t.Run("AuditHashMismatch", func(t *testing.T) {
		header := &types.Header{Number: blockNum}
		block := types.NewBlockWithHeader(header)
		mockRPC.On("BlockByNumber", mock.Anything, blockNum).Return(block, nil).Once()

		rows := sqlmock.NewRows([]string{"hash"}).AddRow("0xwronghash")
		mockDB.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
			WithArgs(blockNum.String()).
			WillReturnRows(rows)

		reconciler.auditBlock(ctx, blockNum)
		assert.NoError(t, mockDB.ExpectationsWereMet())
	})
}

func TestReconciler_performAudit(t *testing.T) {
	db, mockDB, _ := sqlmock.New()
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")

	mockRPC := new(MockRPCClient)
	reconciler := NewReconciler(sqlxDB, mockRPC, GetMetrics())

	ctx := context.Background()

	mockDB.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(100))
	
	// Expect 5 auditBlock calls
	for i := 0; i < 5; i++ {
		checkNum := big.NewInt(int64(100 - i*10))
		header := &types.Header{Number: checkNum}
		block := types.NewBlockWithHeader(header)
		mockRPC.On("BlockByNumber", mock.Anything, checkNum).Return(block, nil).Once()

		rows := sqlmock.NewRows([]string{"hash"}).AddRow(block.Hash().Hex())
		mockDB.ExpectQuery("SELECT hash FROM blocks WHERE number = \\$1").
			WithArgs(checkNum.String()).
			WillReturnRows(rows)
	}

	reconciler.performAudit(ctx, 50)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}
