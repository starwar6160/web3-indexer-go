package engine

// repository_interface.go
// 定义 engine 包所需的数据库操作接口（Repository Pattern）
//
// 设计原则：
//   - engine 包内不直接依赖 *sqlx.DB 或具体 SQL
//   - 所有数据库操作通过此接口进行
//   - 便于集成测试使用 MockRepository 替代真实数据库
//   - 便于未来切换数据库实现（PostgreSQL → SQLite 等）

import (
	"context"
	"math/big"

	"web3-indexer-go/internal/models"
)

// IndexerRepository 定义 engine 包所需的所有数据库操作
// 实现由 internal/database.Repository 提供
type IndexerRepository interface {
	// ─── Checkpoint（同步游标）─────────────────────────────────────────────────

	// GetSyncCursor 获取当前同步游标（已落盘的最高块号）
	GetSyncCursor(ctx context.Context, chainID int64) (int64, error)

	// UpdateSyncCursor 强制更新同步游标（用于自愈、Reorg 等场景）
	UpdateSyncCursor(ctx context.Context, height int64) error

	// UpdateCheckpoint 更新同步检查点（正常同步路径）
	UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error

	// ─── Block（区块）────────────────────────────────────────────────────────

	// GetMaxStoredBlock 获取 blocks 表中最大块号（用于 Watchdog 状态审计）
	GetMaxStoredBlock(ctx context.Context) (int64, error)

	// SaveBlock 插入区块元数据
	SaveBlock(ctx context.Context, block *models.Block) error

	// ─── Transfer（转账）─────────────────────────────────────────────────────

	// SaveTransfer 插入单条转账记录
	SaveTransfer(ctx context.Context, transfer *models.Transfer) error

	// ─── Token Metadata（代币元数据）─────────────────────────────────────────

	// SaveTokenMetadata 持久化代币元数据
	SaveTokenMetadata(meta models.TokenMetadata, address string) error

	// LoadAllMetadata 加载所有已缓存的代币元数据
	LoadAllMetadata() (map[string]models.TokenMetadata, error)

	// UpdateTokenSymbol 更新代币符号
	UpdateTokenSymbol(tokenAddress, symbol string) error

	// ─── 数据清理（Reorg / 自愈）─────────────────────────────────────────────

	// PruneFutureData 删除高于指定高度的所有数据（Reorg 处理）
	PruneFutureData(ctx context.Context, chainHead int64) error
}

// CheckpointReader 只读检查点接口（供 Watchdog 使用）
// 比 IndexerRepository 更窄的接口，遵循接口隔离原则
type CheckpointReader interface {
	GetSyncCursor(ctx context.Context, chainID int64) (int64, error)
	GetMaxStoredBlock(ctx context.Context) (int64, error)
}

// CheckpointWriter 只写检查点接口（供 Processor 使用）
type CheckpointWriter interface {
	UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error
	UpdateSyncCursor(ctx context.Context, height int64) error
}
