package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockIndexerService struct {
	mock.Mock
}

func (m *MockIndexerService) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockIndexerService) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockIndexerService) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockIndexerService) GetCurrentBlock() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockIndexerService) SetLowPowerMode(enabled bool) {
	m.Called(enabled)
}

func TestStateManager_Basic(t *testing.T) {
	os.Setenv("CONTINUOUS_MODE", "false")
	os.Setenv("DISABLE_SMART_SLEEP", "false")
	defer os.Unsetenv("CONTINUOUS_MODE")
	defer os.Unsetenv("DISABLE_SMART_SLEEP")

	mockIndexer := new(MockIndexerService)
	sm := NewStateManager(mockIndexer, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sm.Start(ctx)

	assert.Equal(t, StateIdle, sm.GetState())

	mockIndexer.On("Start", mock.Anything).Return(nil)
	sm.StartDemo()

	// Wait for async transition
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, StateActive, sm.GetState())
}

func TestAdminServer_Routes(t *testing.T) {
	os.Setenv("CONTINUOUS_MODE", "false")
	os.Setenv("DISABLE_SMART_SLEEP", "false")
	defer os.Unsetenv("CONTINUOUS_MODE")
	defer os.Unsetenv("DISABLE_SMART_SLEEP")

	mockIndexer := new(MockIndexerService)
	// RPC Pool needs a valid URL or it might fail initialization if I'm not careful
	// but here I use NewRPCClientPool which does a dial.
	mockRPC := &RPCClientPool{}

	sm := NewStateManager(mockIndexer, mockRPC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sm.Start(ctx)

	admin := NewAdminServer(sm)

	// Set up global expectations because RecordAccess can trigger them anytime
	mockIndexer.On("Start", mock.Anything).Return(nil).Maybe()
	mockIndexer.On("Stop").Return(nil).Maybe()
	mockIndexer.On("IsRunning").Return(false).Maybe()
	mockIndexer.On("GetCurrentBlock").Return("0").Maybe()

	t.Run("GetStatus", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/admin/state", nil)
		rr := httptest.NewRecorder()
		admin.GetStatus(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("StartDemo", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/admin/start-demo", nil)
		rr := httptest.NewRecorder()
		admin.StartDemo(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, StateActive, sm.GetState())
	})

	t.Run("Stop", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/admin/stop", nil)
		rr := httptest.NewRecorder()
		admin.Stop(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, StateIdle, sm.GetState())
	})
}

func TestHealthServer_Routes(t *testing.T) {
	db, mockDB, _ := sqlmock.New()
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")

	mockRPC := &RPCClientPool{}
	health := NewHealthServer(sqlxDB, mockRPC, nil, nil)

	t.Run("Healthz", func(t *testing.T) {
		mockDB.ExpectPing()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		rr := httptest.NewRecorder()
		health.Healthz(rr, req)

		assert.True(t, rr.Code == http.StatusOK || rr.Code == http.StatusServiceUnavailable)
	})

	t.Run("Live", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/healthz/live", nil)
		rr := httptest.NewRecorder()
		health.Live(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}
