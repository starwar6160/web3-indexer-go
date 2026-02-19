package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/jmoiron/sqlx"
)

// 🔥 工业级异步写入器 (AsyncWriter) - 横滨实验室专用版
// 针对 AMD 3800X + 128G RAM + 990 PRO 极致优化
// 核心策略：海量内存缓冲 + 批量事务 + 空块过滤

// PersistTask 携带需要落盘的原始交易数据
type PersistTask struct {
	Height    uint64            // 区块高度
	Block     models.Block      // 区块元数据
	Transfers []models.Transfer // 提取出的转账记录
	Sequence  uint64            // 消息序列号 (用于追踪)
}

// AsyncWriter 负责异步持久化逻辑
type AsyncWriter struct {
	// 1. 输入通道：100,000 深度缓冲，利用 128G 内存彻底消除背压
	taskChan chan PersistTask

	db           *sqlx.DB
	orchestrator *Orchestrator

	// 2. 批处理配置
	batchSize     int
	flushInterval time.Duration

	// 状态控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 性能指标 (原子操作)
	diskWatermark atomic.Uint64
	writeDuration atomic.Int64 // 纳秒
}

// NewAsyncWriter 初始化
func NewAsyncWriter(db *sqlx.DB, o *Orchestrator) *AsyncWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &AsyncWriter{
		// 🚀 16G RAM 调优：将 100,000 下调至 5,000
		taskChan:      make(chan PersistTask, 5000),
		db:            db,
		orchestrator:  o,
		batchSize:     1000, // 990 PRO 顺序写入最佳批次
		flushInterval: 500 * time.Millisecond,
		ctx:           ctx,
		cancel:        cancel,
	}
	return w
}

// Start 启动写入主循环
func (w *AsyncWriter) Start() {
	slog.Info("📝 AsyncWriter: Engine Started",
		"buffer_cap", cap(w.taskChan),
		"batch_size", w.batchSize,
		"flush_interval", w.flushInterval)

	w.wg.Add(1)
	go w.run()
}

func (w *AsyncWriter) run() {
	defer w.wg.Done()

	batch := make([]PersistTask, 0, w.batchSize)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			// 优雅退出：处理剩余任务
			if len(batch) > 0 {
				w.flush(batch)
			}
			return

		case task := <-w.taskChan:
			batch = append(batch, task)
			if len(batch) >= w.batchSize {
				w.flush(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

// flush 执行批量写入事务
func (w *AsyncWriter) flush(batch []PersistTask) {
	if len(batch) == 0 {
		return
	}

	start := time.Now()
	// 开启高性能事务
	tx, err := w.db.BeginTxx(w.ctx, nil)
	if err != nil {
		slog.Error("📝 AsyncWriter: BeginTx failed", "err", err)
		return
	}
	defer tx.Rollback()

	var (
		maxHeight      uint64 = 0
		totalTransfers        = 0
		validBlocks           = 0
	)

	for _, task := range batch {
		if task.Height > maxHeight {
			maxHeight = task.Height
		}

		// 🚀 核心优化：空块过滤
		// 在 Anvil 环境中，95% 以上的块是空的。跳过这些块的 DB 写入可极大提升性能。
		if len(task.Transfers) == 0 {
			continue
		}

		validBlocks++
		totalTransfers += len(task.Transfers)

		// 1. 写入区块元数据
		if _, err := tx.ExecContext(w.ctx,
			`INSERT INTO blocks (number, hash, parent_hash, timestamp, gas_limit, gas_used, transaction_count)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (number) DO NOTHING`,
			task.Block.Number.String(), task.Block.Hash, task.Block.ParentHash,
			task.Block.Timestamp, task.Block.GasLimit, task.Block.GasUsed, task.Block.TransactionCount); err != nil {
			slog.Error("📝 AsyncWriter: Insert block failed", "height", task.Height, "err", err)
			return
		}

		// 2. 批量写入转账记录
		for _, t := range task.Transfers {
			if _, err := tx.ExecContext(w.ctx,
				`INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				 ON CONFLICT DO NOTHING`,
				t.BlockNumber.String(), t.TxHash, t.LogIndex, t.From, t.To, t.Amount.String(), t.TokenAddress, t.Symbol, t.Type); err != nil {
				slog.Error("📝 AsyncWriter: Insert transfer failed", "tx", t.TxHash, "err", err)
				return
			}
		}
	}

	// 3. 更新同步检查点 (SSOT 物理确认)
	if _, err := tx.ExecContext(w.ctx,
		`INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		 VALUES (1, $1)
		 ON CONFLICT (chain_id) DO UPDATE SET
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()`,
		maxHeight); err != nil {
		slog.Error("📝 AsyncWriter: Update checkpoint failed", "err", err)
		return
	}

	// 🚀 Grafana 对齐：更新 sync_status 表
	syncedBlock := int64(maxHeight)
	chainHeight := syncedBlock
	if metrics := GetMetrics(); metrics != nil {
		if h := metrics.lastChainHeight.Load(); h > 0 {
			chainHeight = h
		}
	}
	lag := chainHeight - syncedBlock
	if lag < 0 {
		lag = 0
	}

	if _, err := tx.ExecContext(w.ctx, `
		INSERT INTO sync_status (chain_id, last_synced_block, latest_block, sync_lag, status, updated_at)
		VALUES ($1, $2, $3, $4, 'syncing', NOW())
		ON CONFLICT (chain_id) DO UPDATE SET
			last_synced_block = EXCLUDED.last_synced_block,
			latest_block = EXCLUDED.latest_block,
			sync_lag = EXCLUDED.sync_lag,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, 1, syncedBlock, chainHeight, lag); err != nil {
		slog.Warn("📝 AsyncWriter: Update sync_status failed", "err", err)
	}

	if err := tx.Commit(); err != nil {
		slog.Error("📝 AsyncWriter: Commit failed", "err", err)
		return
	}

	// 更新磁盘水位线
	w.diskWatermark.Store(maxHeight)
	w.writeDuration.Store(int64(time.Since(start)))

	// --- 4. 闭环通知 (SSOT) ---
	// 无论是否写入了数据库（空块也算同步成功），都要通知 Orchestrator
	// 只有收到 CmdCommitDisk，SyncedCursor 才会真正推进
	w.orchestrator.Dispatch(CmdCommitDisk, maxHeight)

	// 性能日志
	dur := time.Since(start)
	if validBlocks > 0 || dur > 100*time.Millisecond {
		slog.Info("📝 AsyncWriter: Batch Flushed",
			"batch_len", len(batch),
			"valid_blocks", validBlocks,
			"transfers", totalTransfers,
			"tip", maxHeight,
			"dur", dur)
	}
}

// Enqueue 提交持久化任务 (非阻塞)
func (w *AsyncWriter) Enqueue(task PersistTask) error {
	select {
	case w.taskChan <- task:
		return nil
	default:
		return sql.ErrConnDone // 简单表示队列满 (实际不应发生)
	}
}

// GetMetrics 获取性能指标
func (w *AsyncWriter) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"disk_watermark":    w.diskWatermark.Load(),
		"write_duration_ms": time.Duration(w.writeDuration.Load()).Milliseconds(),
		"queue_depth":       len(w.taskChan),
	}
}

// Shutdown 优雅关闭
func (w *AsyncWriter) Shutdown(timeout time.Duration) error {
	slog.Info("📝 AsyncWriter: Shutting down...")
	w.cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}
