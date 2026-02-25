package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// NewAsyncWriter 初始化
func NewAsyncWriter(db *sqlx.DB, o *Orchestrator, ephemeral bool, chainID int64) *AsyncWriter {
	ctx, cancel := context.WithCancel(context.Background())
	return &AsyncWriter{
		taskChan:      make(chan PersistTask, 15000),
		db:            db,
		orchestrator:  o,
		chainID:       chainID,
		ephemeralMode: ephemeral,
		batchSize:     200,
		flushInterval: 500 * time.Millisecond,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start 启动写入主循环
func (w *AsyncWriter) Start() {
	slog.Info("📝 AsyncWriter: Engine Started",
		"buffer_cap", cap(w.taskChan),
		"batch_size", w.batchSize)
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
			if len(batch) > 0 {
				w.flush(batch)
			}
			return
		case task := <-w.taskChan:
			if len(w.taskChan) > cap(w.taskChan)*90/100 {
				w.emergencyDrain()
				batch = batch[:0]
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

// Enqueue 提交持久化任务
func (w *AsyncWriter) Enqueue(task PersistTask) error {
	select {
	case w.taskChan <- task:
		return nil
	default:
		return context.DeadlineExceeded
	}
}

// Shutdown 优雅关闭
func (w *AsyncWriter) Shutdown(timeout time.Duration) error {
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

// GetMetrics 获取性能指标
func (w *AsyncWriter) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"disk_watermark":    w.diskWatermark.Load(),
		"write_duration_ms": time.Duration(w.writeDuration.Load()).Milliseconds(),
		"queue_depth":       len(w.taskChan),
	}
}
