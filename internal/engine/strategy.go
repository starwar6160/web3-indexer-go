package engine

import (
	"context"
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
	slog.Warn("🚨 Strategy: ANVIL mode detected. Executing HARD RESET (Memory & DB).")
	
	// 1. 物理清空数据库 (20 年老兵的暴力美学)
	if db != nil {
		_, err := db.ExecContext(ctx, "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints; DELETE FROM sync_status;")
		if err != nil {
			slog.Error("🚨 Hard Reset FAILED: Database truncate error", "err", err)
		} else {
			slog.Info("✨ Hard Reset: Database wiped clean.")
		}
	}

	// 2. 内存游标归零
	o.ResetToZero()
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
