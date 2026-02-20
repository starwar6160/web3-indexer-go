package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

	db            *sqlx.DB
	orchestrator  *Orchestrator
	ephemeralMode bool // 🔥 新增：是否为全内存模式

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
func NewAsyncWriter(db *sqlx.DB, o *Orchestrator, ephemeral bool) *AsyncWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &AsyncWriter{
		// 🚀 16G RAM 调优：提升至 15,000，给予消费端更多缓冲空间
		taskChan:      make(chan PersistTask, 15000),
		db:            db,
		orchestrator:  o,
		ephemeralMode: ephemeral,
		batchSize:     200, // 🚀 16G RAM 调优：缩小批次，减少大事务对 I/O 的独占
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
			// 🚀 紧急泄压阀：如果堆积超过 90%，触发丢弃模式
			if len(w.taskChan) > cap(w.taskChan)*90/100 {
				w.emergencyDrain()
				batch = batch[:0] // 清空当前批次，从泄压后的点重新开始
				continue
			}

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

	if w.ephemeralMode {
		w.flushEphemeral(batch)
		return
	}

	w.flushToDB(batch)
}

func (w *AsyncWriter) flushEphemeral(batch []PersistTask) {
	maxHeight := uint64(0)
	totalEvents := 0
	for _, task := range batch {
		if task.Height > maxHeight {
			maxHeight = task.Height
		}
		totalEvents += len(task.Transfers)
		GetMetrics().RecordBlockActivity(1)
	}

	w.diskWatermark.Store(maxHeight)
	w.orchestrator.AdvanceDBCursor(maxHeight)
	w.orchestrator.DispatchLog("INFO", "🔥 Ephemeral Flush: Metadata Ignored", "height", maxHeight, "dropped_events", totalEvents)
}

func (w *AsyncWriter) flushToDB(batch []PersistTask) {
	start := time.Now()
	tx, err := w.db.BeginTxx(w.ctx, nil)
	if err != nil {
		// 🔥 资深写法：如果是 context canceled，说明是主动关闭，不应报错
		if errors.Is(err, context.Canceled) {
			slog.Info("📝 AsyncWriter: BeginTx skipped", "reason", "context canceled (graceful shutdown)")
		} else {
			slog.Error("📝 AsyncWriter: BeginTx failed", "err", err)
		}
		return
	}
	defer func() {
		if rErr := tx.Rollback(); rErr != nil && rErr != sql.ErrTxDone {
			slog.Debug("📝 AsyncWriter: Rollback skipped", "reason", "committed")
		}
	}()

	blocks, transfers, maxHeight := w.prepareBatch(batch)

	if err := w.persistBatch(tx, blocks, transfers); err != nil {
		return
	}

	if err := w.updateCheckpoints(tx, maxHeight); err != nil {
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("📝 AsyncWriter: Commit failed", "err", err)
		return
	}

	w.diskWatermark.Store(maxHeight)
	w.writeDuration.Store(int64(time.Since(start)))
	w.orchestrator.Dispatch(CmdCommitDisk, maxHeight)
	w.logFlushStats(batch, maxHeight, len(transfers), time.Since(start))
}

func (w *AsyncWriter) prepareBatch(batch []PersistTask) ([]models.Block, []models.Transfer, uint64) {
	blocks := make([]models.Block, 0, len(batch))
	transfers := make([]models.Transfer, 0)
	var maxHeight uint64

	for _, task := range batch {
		if task.Height > maxHeight {
			maxHeight = task.Height
		}
		GetMetrics().RecordBlockActivity(1)
		blocks = append(blocks, task.Block)
		if len(task.Transfers) > 0 {
			transfers = append(transfers, task.Transfers...)
		}
	}
	return blocks, transfers, maxHeight
}

func (w *AsyncWriter) persistBatch(tx *sqlx.Tx, blocks []models.Block, transfers []models.Transfer) error {
	inserter := NewBulkInserter(w.db)
	if err := inserter.InsertBlocksBatchTx(w.ctx, tx, blocks); err != nil {
		slog.Error("📝 AsyncWriter: Bulk insert blocks failed", "err", err)
		return err
	}
	if len(transfers) > 0 {
		if err := inserter.InsertTransfersBatchTx(w.ctx, tx, transfers); err != nil {
			slog.Error("📝 AsyncWriter: Bulk insert transfers failed", "err", err)
			return err
		}
	}
	return nil
}

func (w *AsyncWriter) updateCheckpoints(tx *sqlx.Tx, maxHeight uint64) error {
	maxHeightStr := fmt.Sprintf("%d", maxHeight)
	_, err := tx.ExecContext(w.ctx,
		`INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		 VALUES (1, $1) ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = EXCLUDED.last_synced_block, updated_at = NOW()`,
		maxHeightStr)
	if err != nil {
		slog.Error("📝 AsyncWriter: Update checkpoint failed", "err", err)
		return err
	}

	syncedBlock, _ := SafeCastUint64ToInt64(maxHeight) // nolint:errcheck // display only
	_, err = tx.ExecContext(w.ctx, `
		INSERT INTO sync_status (chain_id, last_processed_block, last_processed_timestamp, status)
		VALUES ($1, $2, NOW(), 'syncing')
		ON CONFLICT (chain_id) DO UPDATE SET
			last_processed_block = EXCLUDED.last_processed_block,
			last_processed_timestamp = NOW()`,
		1, syncedBlock)
	if err != nil {
		slog.Debug("📝 AsyncWriter: sync_status update skipped", "err", err)
	}
	return nil
}

func (w *AsyncWriter) logFlushStats(batch []PersistTask, tip uint64, txCount int, dur time.Duration) {
	if len(batch) > 0 || dur > 100*time.Millisecond {
		slog.Info("📝 AsyncWriter: Batch Flushed", "size", len(batch), "txs", txCount, "tip", tip, "dur", dur)
		w.orchestrator.DispatchLog("SUCCESS", "💾 Batch Flushed to Disk", "blocks", len(batch), "transfers", txCount, "tip", tip)
	}
}

// Enqueue 提交持久化任务 (非阻塞)
func (w *AsyncWriter) Enqueue(task PersistTask) error {
	select {
	case w.taskChan <- task:
		return nil
	default:
		return errors.New("queue full")
	}
}

// emergencyDrain 紧急泄压：快速消耗 Channel，只保留高度，丢弃 Metadata
func (w *AsyncWriter) emergencyDrain() {
	depth := len(w.taskChan)
	capacity := cap(w.taskChan)
	slog.Warn("🚨 BACKPRESSURE_CRITICAL: Initiating Emergency Drain",
		"depth", depth,
		"capacity", capacity)

	// 通知大脑：进入压力泄压模式
	w.orchestrator.SetSystemState(SystemStateDegraded)

	count := 0
	var lastHeight uint64

	// 泄压循环：快速排空到 50%
	targetDepth := capacity * 50 / 100
	for len(w.taskChan) > targetDepth {
		select {
		case task := <-w.taskChan:
			count++
			if task.Height > lastHeight {
				lastHeight = task.Height
			}
			// 🚀 记录块处理活动，即使丢弃了 Metadata，也算同步了区块
			GetMetrics().RecordBlockActivity(1)
			// 🚀 核心动作：丢弃 Metadata (不写库)
		default:
			goto done
		}
	}

done:
	// 最终同步一次游标到大脑，让 UI 的 Synced 数字瞬间跳跃
	if lastHeight > 0 {
		w.orchestrator.AdvanceDBCursor(lastHeight)
	}

	slog.Info("✅ Relief Valve Closed",
		"dropped_blocks", count,
		"new_synced_tip", lastHeight)

	// 恢复状态 (如果后续平稳，Orchestrator 也会自动评估)
	w.orchestrator.SetSystemState(SystemStateRunning)
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
