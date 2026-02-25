package engine

import (
	"fmt"
	"log/slog"
	"time"

	"web3-indexer-go/internal/models"
)

func (w *AsyncWriter) flush(batch []PersistTask) {
	if len(batch) == 0 {
		return
	}
	start := time.Now()
	if w.ephemeralMode {
		w.handleEphemeralFlush(batch)
		return
	}

	tx, err := w.db.BeginTxx(w.ctx, nil)
	if err != nil {
		slog.Error("📝 AsyncWriter: BeginTx failed", "err", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	var (
		maxHeight         uint64
		transfersToInsert []models.Transfer
		blocksToInsert    []models.Block
	)

	for _, task := range batch {
		if task.Height > maxHeight {
			maxHeight = task.Height
		}
		GetMetrics().RecordBlockActivity(1)
		blocksToInsert = append(blocksToInsert, task.Block)
		transfersToInsert = append(transfersToInsert, task.Transfers...)
	}

	inserter := NewBulkInserter(w.db)
	if err := inserter.InsertBlocksBatchTx(w.ctx, tx, blocksToInsert); err != nil {
		slog.Error("📝 AsyncWriter: Block insert failed", "err", err, "count", len(blocksToInsert))
		// 注意: 不 return，继续尝试插入 transfers，让 tx.Commit() 处理整体失败
	}
	if len(transfersToInsert) > 0 {
		if err := inserter.InsertTransfersBatchTx(w.ctx, tx, transfersToInsert); err != nil {
			slog.Error("📝 AsyncWriter: Transfer insert failed", "err", err, "count", len(transfersToInsert))
			// 注意: 不 return，让 tx.Commit() 处理整体失败
		}
	}

	w.updateCheckpointsTx(tx, maxHeight)

	if err := tx.Commit(); err != nil {
		slog.Error("📝 AsyncWriter: Commit failed", "err", err)
		return
	}

	w.diskWatermark.Store(maxHeight)
	w.writeDuration.Store(int64(time.Since(start)))
	w.orchestrator.Dispatch(CmdCommitDisk, maxHeight)
}

func (w *AsyncWriter) handleEphemeralFlush(batch []PersistTask) {
	maxHeight := uint64(0)
	for _, task := range batch {
		if task.Height > maxHeight {
			maxHeight = task.Height
		}
		GetMetrics().RecordBlockActivity(1)
	}
	w.diskWatermark.Store(maxHeight)
	w.orchestrator.AdvanceDBCursor(maxHeight)
}

func (w *AsyncWriter) updateCheckpointsTx(tx execer, maxHeight uint64) {
	maxHeightStr := fmt.Sprintf("%d", maxHeight)
	_, err := tx.ExecContext(w.ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2) ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = EXCLUDED.last_synced_block, updated_at = NOW()`,
		w.chainID, maxHeightStr)
	if err != nil {
		slog.Error("📝 AsyncWriter: Checkpoint update failed", "err", err, "maxHeight", maxHeight)
	}

	// 🛡️ 防御性位掩码：确保 uint64 → int64 转换时不会溢出
	// 0x7FFFFFFFFFFFFFFF 是正 int64 的最大值，用于截断溢出的高位
	// 这在处理超大区块号或异常数据时提供安全保护
	syncedBlock := int64(maxHeight & 0x7FFFFFFFFFFFFFFF)
	snap := w.orchestrator.GetSnapshot()
	latestBlock := int64(snap.LatestHeight & 0x7FFFFFFFFFFFFFFF)
	_, err = tx.ExecContext(w.ctx, `
		INSERT INTO sync_status (chain_id, last_synced_block, latest_block, sync_lag, status, last_processed_block, last_processed_timestamp)
		VALUES ($1, $2, $3, $4, 'syncing', $5, NOW())
		ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = EXCLUDED.last_synced_block, latest_block = EXCLUDED.latest_block, sync_lag = EXCLUDED.sync_lag, last_processed_block = EXCLUDED.last_processed_block`,
		w.chainID, syncedBlock, latestBlock, latestBlock-syncedBlock, syncedBlock)
	if err != nil {
		slog.Error("📝 AsyncWriter: Sync status update failed", "err", err, "syncedBlock", syncedBlock, "latestBlock", latestBlock)
	}
}

func (w *AsyncWriter) emergencyDrain() {
	if w.emergencyDrainCooldown.Swap(true) {
		return // 正在冷却中，防止频繁触发
	}
	// 1 分钟后清除冷却标志
	go func() {
		time.Sleep(1 * time.Minute)
		w.emergencyDrainCooldown.Store(false)
	}()

	capacity := cap(w.taskChan)
	w.orchestrator.SetSystemState(SystemStateDegraded)
	var lastHeight uint64
	targetDepth := capacity * 50 / 100
	for len(w.taskChan) > targetDepth {
		select {
		case task := <-w.taskChan:
			if task.Height > lastHeight {
				lastHeight = task.Height
			}
			GetMetrics().RecordBlockActivity(1)
		default:
			goto done
		}
	}
done:
	if lastHeight > 0 {
		w.orchestrator.AdvanceDBCursor(lastHeight)
	}
	w.orchestrator.SetSystemState(SystemStateRunning)
}
