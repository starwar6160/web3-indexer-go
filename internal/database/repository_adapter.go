package database

// repository_adapter.go
// 将 database.Repository 适配到 engine.IndexerRepository 接口
// 补充 engine 接口所需但原 Repository 未实现的方法

import (
	"context"
	"fmt"
)

// GetSyncCursor 获取当前同步游标（已落盘的最高块号）
// 先查 sync_checkpoints，再 fallback 到 blocks 表
func (r *Repository) GetSyncCursor(ctx context.Context, chainID int64) (int64, error) {
	var lastSynced string
	err := r.db.GetContext(ctx, &lastSynced,
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
	if err == nil && lastSynced != "" && lastSynced != "0" {
		var h int64
		if _, scanErr := fmt.Sscanf(lastSynced, "%d", &h); scanErr == nil {
			return h, nil
		}
	}

	// Fallback：从 blocks 表获取最大值
	var maxBlock int64
	if err2 := r.db.GetContext(ctx, &maxBlock, "SELECT COALESCE(MAX(number), 0) FROM blocks"); err2 != nil {
		return 0, nil
	}
	return maxBlock, nil
}

// GetMaxStoredBlock 获取 blocks 表中最大块号
func (r *Repository) GetMaxStoredBlock(ctx context.Context) (int64, error) {
	var maxBlock int64
	err := r.db.GetContext(ctx, &maxBlock, "SELECT COALESCE(MAX(number), 0) FROM blocks")
	if err != nil {
		return 0, err
	}
	return maxBlock, nil
}
