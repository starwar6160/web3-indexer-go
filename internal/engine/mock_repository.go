package engine

// mock_repository.go
// MockRepository 供集成测试使用，无需真实数据库
// 实现 IndexerRepository 接口

import (
	"context"
	"math/big"
	"sync"

	"web3-indexer-go/internal/models"
)

// MockRepository 内存实现的 Repository（仅用于测试）
type MockRepository struct {
	mu sync.RWMutex

	syncCursor    int64
	maxBlock      int64
	blocks        map[uint64]*models.Block
	transfers     []*models.Transfer
	tokenMetadata map[string]models.TokenMetadata

	// 调用计数（用于断言）
	CallCounts map[string]int
}

// NewMockRepository 创建测试用 MockRepository
func NewMockRepository() *MockRepository {
	return &MockRepository{
		blocks:        make(map[uint64]*models.Block),
		tokenMetadata: make(map[string]models.TokenMetadata),
		CallCounts:    make(map[string]int),
	}
}

func (m *MockRepository) record(method string) {
	m.mu.Lock()
	m.CallCounts[method]++
	m.mu.Unlock()
}

// GetSyncCursor 返回当前同步游标
func (m *MockRepository) GetSyncCursor(_ context.Context, _ int64) (int64, error) {
	m.record("GetSyncCursor")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.syncCursor, nil
}

// UpdateSyncCursor 更新同步游标
func (m *MockRepository) UpdateSyncCursor(_ context.Context, height int64) error {
	m.record("UpdateSyncCursor")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncCursor = height
	if height > m.maxBlock {
		m.maxBlock = height
	}
	return nil
}

// UpdateCheckpoint 更新检查点
func (m *MockRepository) UpdateCheckpoint(_ context.Context, _ int64, blockNumber *big.Int) error {
	m.record("UpdateCheckpoint")
	m.mu.Lock()
	defer m.mu.Unlock()
	h := blockNumber.Int64()
	if h > m.syncCursor {
		m.syncCursor = h
	}
	return nil
}

// GetMaxStoredBlock 返回最大存储块号
func (m *MockRepository) GetMaxStoredBlock(_ context.Context) (int64, error) {
	m.record("GetMaxStoredBlock")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxBlock, nil
}

// SaveBlock 保存区块
func (m *MockRepository) SaveBlock(_ context.Context, block *models.Block) error {
	m.record("SaveBlock")
	m.mu.Lock()
	defer m.mu.Unlock()
	var blockNum int64
	if block.Number.Int != nil {
		blockNum = block.Number.Int64()
	}
	m.blocks[uint64(blockNum)] = block //nolint:gosec // blockNum is bounded by chain height
	if blockNum > m.maxBlock {
		m.maxBlock = blockNum
	}
	return nil
}

// SaveTransfer 保存转账记录
func (m *MockRepository) SaveTransfer(_ context.Context, transfer *models.Transfer) error {
	m.record("SaveTransfer")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transfers = append(m.transfers, transfer)
	return nil
}

// SaveTokenMetadata 保存代币元数据
func (m *MockRepository) SaveTokenMetadata(meta models.TokenMetadata, address string) error {
	m.record("SaveTokenMetadata")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenMetadata[address] = meta
	return nil
}

// LoadAllMetadata 加载所有代币元数据
func (m *MockRepository) LoadAllMetadata() (map[string]models.TokenMetadata, error) {
	m.record("LoadAllMetadata")
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]models.TokenMetadata, len(m.tokenMetadata))
	for k, v := range m.tokenMetadata {
		result[k] = v
	}
	return result, nil
}

// UpdateTokenSymbol 更新代币符号
func (m *MockRepository) UpdateTokenSymbol(_ string, _ string) error {
	m.record("UpdateTokenSymbol")
	return nil
}

// PruneFutureData 删除未来数据
func (m *MockRepository) PruneFutureData(_ context.Context, chainHead int64) error {
	m.record("PruneFutureData")
	m.mu.Lock()
	defer m.mu.Unlock()
	for num := range m.blocks {
		// #nosec G115 - block map keys are bounded by chain height, safe to cast
		if int64(num) > chainHead {
			delete(m.blocks, num)
		}
	}
	if m.syncCursor > chainHead {
		m.syncCursor = chainHead
	}
	if m.maxBlock > chainHead {
		m.maxBlock = chainHead
	}
	return nil
}

// ─── 测试辅助方法 ─────────────────────────────────────────────────────────────

// SetSyncCursor 直接设置游标（测试用）
func (m *MockRepository) SetSyncCursor(height int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncCursor = height
}

// GetBlockCount 返回已存储的区块数量
func (m *MockRepository) GetBlockCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.blocks)
}

// GetTransferCount 返回已存储的转账数量
func (m *MockRepository) GetTransferCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.transfers)
}
