package engine

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"github.com/jmoiron/sqlx"
)

// updateCheckpointInTx 在事务内更新 checkpoint（保证原子性）
func (p *Processor) updateCheckpointInTx(ctx context.Context, tx *sqlx.Tx, chainID int64, blockNumber *big.Int) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()
	`, chainID, blockNumber.String())

	if err != nil {
		return fmt.Errorf("failed to update checkpoint: %w", err)
	}

	// 同时更新 sync_status 表，以便 Grafana 展示。
	// latest_block 使用 Metrics 中缓存的链上高度（由 UpdateChainHeight 写入），
	// 而非 MAX(blocks.number)——后者在同步严重滞后时与 last_synced_block 几乎相等，
	// 导致 sync_lag 虚报为 0。
	syncedBlock := blockNumber.Int64()
	chainHeight := syncedBlock // fallback: 若链高度尚未获取，lag 显示为 0 而非负数
	if p.metrics != nil {
		if h := p.metrics.lastChainHeight.Load(); h > 0 {
			chainHeight = h
		}
	}
	lag := chainHeight - syncedBlock
	if lag < 0 {
		lag = 0
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sync_status (chain_id, last_synced_block, latest_block, sync_lag, status, updated_at)
		VALUES ($1, $2, $3, $4, 'syncing', NOW())
		ON CONFLICT (chain_id) DO UPDATE SET
			last_synced_block = EXCLUDED.last_synced_block,
			latest_block = EXCLUDED.latest_block,
			sync_lag = EXCLUDED.sync_lag,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, chainID, syncedBlock, chainHeight, lag)

	if err != nil {
		// 记录错误但不中断流程
		Logger.Warn("failed_to_update_sync_status", "error", err)
	}

	if p.metrics != nil {
		p.metrics.RecordCheckpointUpdate()
	}

	// Keep HeightOracle in sync so /api/status reads a consistent snapshot
	// without making a live RPC call.
	GetHeightOracle().SetIndexedHead(syncedBlock)

	return nil
}

// UpdateCheckpoint 更新同步检查点（已废弃，保留用于兼容性）
// 警告：此方法在事务外调用，存在数据不一致风险，建议统一使用事务内更新
func (p *Processor) UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			Logger.Warn("checkpoint_rollback_failed", "err", err)
		}
	}()

	if err := p.updateCheckpointInTx(ctx, tx, chainID, blockNumber); err != nil {
		return fmt.Errorf("failed to update checkpoint: %w", err)
	}

	return tx.Commit()
}
