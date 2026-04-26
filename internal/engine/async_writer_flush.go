package engine

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
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

	// 🔥 FINDING-9 修复：在事务开始前捕获快照，保证 latestHeight 与批次数据一致
	snap := w.orchestrator.GetSnapshot()
	latestHeight := snap.LatestHeight

	tx, err := w.db.BeginTxx(w.ctx, nil)
	if err != nil {
		slog.Error("📝 AsyncWriter: BeginTx failed", "err", err)
		return
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			slog.Error("📝 AsyncWriter: Rollback failed", "err", rbErr)
		}
	}()

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

	w.updateCheckpointsTx(tx, maxHeight, latestHeight)

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

func (w *AsyncWriter) updateCheckpointsTx(tx execer, maxHeight uint64, latestHeight uint64) {
	maxHeightStr := fmt.Sprintf("%d", maxHeight)
	_, err := tx.ExecContext(w.ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2) ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = EXCLUDED.last_synced_block, updated_at = NOW()`,
		w.chainID, maxHeightStr)
	if err != nil {
		slog.Error("📝 AsyncWriter: Checkpoint update failed", "err", err, "maxHeight", maxHeight)
	}

	// 🛡️ 防御性位掩码：确保 uint64 → int64 转换时不会溢出
	syncedBlock := SafeUint64ToInt64(maxHeight & uint64(math.MaxInt64))
	// 🔥 FINDING-9 修复：latestBlock 从 flush 入口处的快照获取，保证与事务原子性一致
	latestBlock := SafeUint64ToInt64(latestHeight & uint64(math.MaxInt64))
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
	const emergencyDrainCooldown = 60 * time.Second
	go func() {
		time.Sleep(emergencyDrainCooldown)
		w.emergencyDrainCooldown.Store(false)
	}()

	w.orchestrator.SetSystemState(SystemStateDegraded)

	// 🔥 FINDING-5 修复：收集所有排出的任务并批量写入 DB，而非丢弃
	// 旧代码只推进游标不写入数据，导致永久性数据空洞
	targetDepth := cap(w.taskChan) * 50 / 100
	megaBatch := make([]PersistTask, 0, cap(w.taskChan)-targetDepth)
	for len(w.taskChan) > targetDepth {
		select {
		case task := <-w.taskChan:
			megaBatch = append(megaBatch, task)
		default:
			goto flushAll
		}
	}
flushAll:
	if len(megaBatch) > 0 {
		slog.Warn("📝 AsyncWriter: Emergency drain → flushing to DB",
			"drained_tasks", len(megaBatch))
		w.flush(megaBatch)
	}
	w.orchestrator.SetSystemState(SystemStateRunning)
}
