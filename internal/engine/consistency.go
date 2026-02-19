package engine

import (
	"context"
	"fmt"
	"log/slog"
	"web3-indexer-go/internal/models"

	"github.com/jmoiron/sqlx"
)

// RepositoryAdapterWrapper wraps sqlx.DB to implement DBUpdater interface for the guard
type RepositoryAdapterWrapper struct {
	DB *sqlx.DB
}

func (r *RepositoryAdapterWrapper) UpdateTokenSymbol(_, _ string) error         { return nil }
func (r *RepositoryAdapterWrapper) UpdateTokenDecimals(_ string, _ uint8) error { return nil }
func (r *RepositoryAdapterWrapper) SaveTokenMetadata(_ models.TokenMetadata, _ string) error {
	return nil
}
func (r *RepositoryAdapterWrapper) LoadAllMetadata() (map[string]models.TokenMetadata, error) {
	return nil, nil
}

func (r *RepositoryAdapterWrapper) UpdateSyncCursor(ctx context.Context, height int64) error {
	headStr := fmt.Sprintf("%d", height)
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck // Rollback is used for safety, error is expected if commit succeeded.

	if _, err := tx.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1, updated_at = NOW()", headStr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_status SET last_processed_block = $1, last_processed_timestamp = NOW()", headStr); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *RepositoryAdapterWrapper) GetMaxStoredBlock(ctx context.Context) (int64, error) {
	var dbMax int64
	err := r.DB.GetContext(ctx, &dbMax, "SELECT COALESCE(MAX(number), 0) FROM blocks")
	return dbMax, err
}

func (r *RepositoryAdapterWrapper) PruneFutureData(ctx context.Context, chainHead int64) error {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck // Rollback is standard practice, error is non-critical here

	headStr := fmt.Sprintf("%d", chainHead)

	if _, err := tx.ExecContext(ctx, "DELETE FROM transfers WHERE block_number > $1", headStr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE number > $1", headStr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1, updated_at = NOW()", headStr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_status SET last_processed_block = $1, last_processed_timestamp = NOW()", headStr); err != nil {
		return err
	}
	return tx.Commit()
}

// ConsistencyGuard handles data alignment between DB and Chain
type ConsistencyGuard struct {
	repo     DBUpdater
	rpcPool  RPCClient
	logger   *slog.Logger
	demoMode bool                                             // 🚀 Leap-Sync toggle
	OnStatus func(status string, detail string, progress int) // 🚀 UI feedback callback
}

func NewConsistencyGuard(repo DBUpdater, rpcPool RPCClient) *ConsistencyGuard {
	return &ConsistencyGuard{
		repo:    repo,
		rpcPool: rpcPool,
		logger:  slog.Default(),
	}
}

// SetDemoMode enables/disables Leap-Sync logic
func (g *ConsistencyGuard) SetDemoMode(enabled bool) {
	g.demoMode = enabled
}

// PerformLinearityCheck 检查并修复数据越位问题
func (g *ConsistencyGuard) PerformLinearityCheck(ctx context.Context) error {
	if g.OnStatus != nil {
		g.OnStatus("CHECKING", "Verifying data linearity...", 0)
	}

	// 1. 获取链上最新高度
	chainHead, err := g.rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain height for linearity check: %w", err)
	}

	// 2. 获取数据库最高高度
	dbMax, err := g.repo.GetMaxStoredBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get db height for linearity check: %w", err)
	}

	g.logger.Info("📊 [Linearity] Height comparison", "rpc", chainHead.String(), "db", dbMax)

	// 3. 穿越判定: 如果数据库已经跑到了链的前面
	if dbMax > chainHead.Int64() {
		diff := dbMax - chainHead.Int64()
		g.logger.Warn("🚨 DATA_OVERRUN_DETECTED (Time Travel)",
			"db_height", dbMax,
			"chain_head", chainHead.String(),
			"surplus", diff)

		if g.OnStatus != nil {
			g.OnStatus("REPAIRING", fmt.Sprintf("Pruning %d future blocks to align with chain...", diff), 50)
		}

		// 4. 执行物理剪枝 (Pruning)
		if err := g.repo.PruneFutureData(ctx, chainHead.Int64()); err != nil {
			return fmt.Errorf("pruning failed: %w", err)
		}

		// 🚀 工业级对齐：立即更新 Prometheus Gauge，防止 Grafana 显示残留高度
		metrics := GetMetrics()
		metrics.UpdateCurrentSyncHeight(chainHead.Int64())

		g.logger.Info("✅ [Linearity] Pruning complete. Database aligned with current chain head.", "new_height", chainHead.Int64())
	}

	// 🚀 4. 深度断层判定 (Leap-Sync): 如果链头远超数据库
	if g.demoMode && chainHead.Int64() > dbMax+1000 {
		g.logger.Warn("🚧 [Linearity] Large gap detected in Demo Mode! Executing State Collapse (Leap-Sync).",
			"gap", chainHead.Int64()-dbMax)

		if g.OnStatus != nil {
			g.OnStatus("LEAPING", "Collapsing state to chain head...", 75)
		}

		if err := g.repo.UpdateSyncCursor(ctx, chainHead.Int64()-1); err != nil {
			return fmt.Errorf("leap-sync failed: %w", err)
		}

		g.logger.Info("✅ [Linearity] Leap-Sync complete. System teleported to chain head.")
	}

	if g.OnStatus != nil {
		g.OnStatus("ALIGNED", "Database synchronized.", 100)
	}
	return nil
}
