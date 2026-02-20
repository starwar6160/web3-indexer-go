package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"golang.org/x/time/rate"
)

// Strategy 定义了不同运行环境下的行为差异
type Strategy interface {
	Name() string
	OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, chainID int64) error
	ShouldPersist() bool
	GetConfirmations() uint64
	GetBatchSize() int
	// 🔥 新增：环境特定的性能参数
	GetRPCConfig() (rate.Limit, int) // (QPS limit, Burst)
	GetBackpressureThreshold() int   // 背压触发阈值
	GetSeqBufferSize() int           // Sequencer 缓冲区大小
}

// AnvilStrategy: 针对本地开发优化（极速、易失、0 确认）
type AnvilStrategy struct{}

func (s *AnvilStrategy) Name() string { return "EPHEMERAL_ANVIL" }

func (s *AnvilStrategy) OnStartup(ctx context.Context, o *Orchestrator, db *sqlx.DB, _ int64) error {
	slog.Warn("☢️ ANVIL_EPHEMERAL: Executing Nuclear Reset...")

	// 🚀 Step 0 - Reality Check BEFORE nuclear reset
	if o.fetcher != nil && o.fetcher.pool != nil {
		rpcHeight, err := o.fetcher.pool.GetLatestBlockNumber(ctx)
		if err == nil {
			snap := o.GetSnapshot()

			// Detect "Future Human" state
			if snap.SyncedCursor > rpcHeight.Uint64() ||
				snap.FetchedHeight > rpcHeight.Uint64() ||
				snap.LatestHeight > rpcHeight.Uint64() {

				gap := int64(0)
				if snap.SyncedCursor > rpcHeight.Uint64() {
					// #nosec G115 - gap only used for logging, overflow doesn't affect core logic
					gap = int64(snap.SyncedCursor) - rpcHeight.Int64()
				}

				slog.Error("🚨 REALITY_PARADOX_DETECTED: Indexer is in the future!",
					"mem_synced", snap.SyncedCursor,
					"mem_fetched", snap.FetchedHeight,
					"mem_latest", snap.LatestHeight,
					"rpc_actual", rpcHeight.Uint64(),
					"reality_gap", gap,
					"action", "forcing_collapse_to_reality")

				// Force collapse to RPC reality
				o.SnapToReality(rpcHeight.Uint64())
			} else {
				slog.Info("✅ REALITY_CHECK: State aligned with RPC",
					"rpc_height", rpcHeight.Uint64(),
					"mem_height", snap.SyncedCursor)
			}
		}
	}

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

func (s *AnvilStrategy) ShouldPersist() bool      { return false } // 🔥 Anvil 不写盘，彻底释放 5600U I/O
func (s *AnvilStrategy) GetConfirmations() uint64 { return 0 }
func (s *AnvilStrategy) GetBatchSize() int        { return 200 }

// 🔥 Anvil 模式：全速运行，12 核算力全开
func (s *AnvilStrategy) GetRPCConfig() (rate.Limit, int) {
	return rate.Limit(1000), 100 // 1000 QPS, Burst 100
}

func (s *AnvilStrategy) GetBackpressureThreshold() int {
	return 5000 // 16G 内存，不怕积压
}

func (s *AnvilStrategy) GetSeqBufferSize() int {
	return 10000 // 大缓冲区
}

// TestnetStrategy: 针对测试网优化（稳健、持久、断点续传）
type TestnetStrategy struct{}

func (s *TestnetStrategy) Name() string { return "PERSISTENT_TESTNET" }

func (s *TestnetStrategy) OnStartup(_ context.Context, o *Orchestrator, db *sqlx.DB, chainID int64) error {
	slog.Info("💾 Strategy: TESTNET mode detected. Aligning with disk cursor.")
	return o.LoadInitialStateFromDB(db, chainID)
}

func (s *TestnetStrategy) ShouldPersist() bool      { return true }
func (s *TestnetStrategy) GetConfirmations() uint64 { return 6 } // 等待 6 个块确认
func (s *TestnetStrategy) GetBatchSize() int        { return 50 }

// 🔥 Testnet 模式：配额敏感，保守运行
func (s *TestnetStrategy) GetRPCConfig() (rate.Limit, int) {
	return rate.Limit(2), 1 // 2 QPS, Burst 1 (省钱模式)
}

func (s *TestnetStrategy) GetBackpressureThreshold() int {
	return 100 // 内存敏感，快速触发节流
}

func (s *TestnetStrategy) GetSeqBufferSize() int {
	return 500 // 小缓冲区
}

// GetStrategy 根据 ChainID 自动选择策略
func GetStrategy(chainID int64) Strategy {
	if chainID == 31337 {
		return &AnvilStrategy{}
	}
	return &TestnetStrategy{}
}
