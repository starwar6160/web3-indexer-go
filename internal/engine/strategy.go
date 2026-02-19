package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// EngineStrategy 定义了不同运行环境下的行为差异
type EngineStrategy interface {
	Name() string
	OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, chainID int64) error
	ShouldPersist() bool
	GetConfirmations() uint64
	GetBatchSize() int
}

// AnvilStrategy: 针对本地开发优化（极速、易失、0 确认）
type AnvilStrategy struct{}

func (s *AnvilStrategy) Name() string { return "EPHEMERAL_ANVIL" }

func (s *AnvilStrategy) OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, _ int64) error {
	slog.Warn("☢️ ANVIL_EPHEMERAL: Executing Nuclear Reset...")

	// 1. 物理清空数据库 (TRUNCATE 是最彻底的)
	if db != nil {
		tables := []string{"blocks", "transfers", "sync_checkpoints", "sync_status", "visitor_stats"}
		for _, table := range tables {
			_, err := db.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
			if err != nil {
				slog.Debug("🚨 Strategy: Truncate failed (ignoring)", "table", table, "err", err)
			}
		}
		slog.Info("✨ Hard Reset: Database physically pulverized.")
	}

	// 2. 内存原子级归零
	o.ResetToZero()
	
	// 3. 清空管道残留
	if o.fetcher != nil {
		o.fetcher.ClearJobs()
	}

	slog.Info("✅ Nuclear Reset Complete. System is logically pure.")
	return nil
}

func (s *AnvilStrategy) ShouldPersist() bool { return false } // 🔥 Anvil 不写盘，彻底释放 5600U I/O
func (s *AnvilStrategy) GetConfirmations() uint64 { return 0 }
func (s *AnvilStrategy) GetBatchSize() int { return 200 }

// TestnetStrategy: 针对测试网优化（稳健、持久、断点续传）
type TestnetStrategy struct{}

func (s *TestnetStrategy) Name() string { return "PERSISTENT_TESTNET" }

func (s *TestnetStrategy) OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, chainID int64) error {
	slog.Info("💾 Strategy: TESTNET mode detected. Aligning with disk cursor.")
	return o.LoadInitialState(db, chainID)
}

func (s *TestnetStrategy) ShouldPersist() bool { return true }
func (s *TestnetStrategy) GetConfirmations() uint64 { return 6 } // 等待 6 个块确认
func (s *TestnetStrategy) GetBatchSize() int { return 50 }

// GetStrategy 根据 ChainID 自动选择策略
func GetStrategy(chainID int64) EngineStrategy {
	if chainID == 31337 {
		return &AnvilStrategy{}
	}
	return &TestnetStrategy{}
}
