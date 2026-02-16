package engine

import (
	"context"
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

	// 同时更新 sync_status 表，以便 Grafana 展示
	// 这里通过 SELECT 子查询实时计算最新块和延迟，避免额外的 RPC 调用
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sync_status (chain_id, last_synced_block, latest_block, sync_lag, status, updated_at)
		SELECT 
			$1, 
			$2::BIGINT, 
			COALESCE(MAX(number), $2::BIGINT), 
			GREATEST(0, COALESCE(MAX(number), $2::BIGINT) - $2::BIGINT),
			'syncing',
			NOW()
		FROM blocks
		ON CONFLICT (chain_id) DO UPDATE SET
			last_synced_block = EXCLUDED.last_synced_block,
			latest_block = EXCLUDED.latest_block,
			sync_lag = EXCLUDED.sync_lag,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, chainID, blockNumber.String())

	if err != nil {
		// 记录错误但不中断流程
		Logger.Warn("failed_to_update_sync_status", "error", err)
	}

	if p.metrics != nil {
		p.metrics.RecordCheckpointUpdate()
	}

	return nil
}

// UpdateCheckpoint 更新同步检查点（已废弃，保留用于兼容性）
// 警告：此方法在事务外调用，存在数据不一致风险，建议统一使用事务内更新
func (p *Processor) UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := p.updateCheckpointInTx(ctx, tx, chainID, blockNumber); err != nil {
		return fmt.Errorf("failed to update checkpoint: %w", err)
	}

	return tx.Commit()
}
