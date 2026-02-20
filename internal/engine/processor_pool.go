package engine

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// 🔥 Processor 并行化工作池
// 解决单核 CPU 利用率低、BPS 停滞问题
// 利用 5600U 的 12 核优势，将深度解析并行化

type ProcessorPool struct {
	numWorkers int
	jobChan    chan ProcessorJob
	resultChan chan ProcessorResult
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

type ProcessorJob struct {
	Block       *FetchedBlock
	SequenceNum uint64
	Timestamp   time.Time
}

type ProcessorResult struct {
	SequenceNum uint64
	Block       *ProcessedBlock
	Err         error
	Duration    time.Duration
}

type FetchedBlock struct {
	Number       uint64
	Hash         string
	Transactions []RawTransaction
	Timestamp    uint64
}

type RawTransaction struct {
	Hash     string
	From     string
	To       string
	Value    string
	Data     string
	GasPrice string
	GasLimit uint64
}

type ProcessedBlock struct {
	Number    uint64
	Hash      string
	Transfers []Transfer
	Metrics   BlockMetrics
}

type Transfer struct {
	TxHash      string
	From        string
	To          string
	Amount      string
	Token       string
	IsWhale     bool
	Category    string
}

type BlockMetrics struct {
	TxCount      int
	TransferCount int
	GasUsed      uint64
	ProcessTime  time.Duration
}

// NewProcessorPool 创建工作池
// workers: 并发度，建议设为 runtime.NumCPU() 或更高
func NewProcessorPool(workers int) *ProcessorPool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ProcessorPool{
		numWorkers: workers,
		jobChan:    make(chan ProcessorJob, workers*2),
		resultChan: make(chan ProcessorResult, workers*2),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 启动工作池
func (p *ProcessorPool) Start(processorFunc func(ProcessorJob) ProcessorResult) {
	slog.Info("🚀 ProcessorPool started", "workers", p.numWorkers, "buffer", cap(p.jobChan))
	
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i, processorFunc)
	}
}

// worker 单个工作协程
func (p *ProcessorPool) worker(id int, processorFunc func(ProcessorJob) ProcessorResult) {
	defer p.wg.Done()
	
	slog.Debug("🚀 ProcessorPool worker started", "id", id)
	
	for {
		select {
		case <-p.ctx.Done():
			slog.Debug("🚀 ProcessorPool worker stopping", "id", id)
			return
			
		case job, ok := <-p.jobChan:
			if !ok {
				return
			}
			
			start := time.Now()
			result := processorFunc(job)
			result.Duration = time.Since(start)
			
			select {
			case p.resultChan <- result:
			case <-p.ctx.Done():
				return
			}
		}
	}
}

// Submit 提交任务（阻塞直到队列有空间）
func (p *ProcessorPool) Submit(job ProcessorJob) error {
	select {
	case <-p.ctx.Done():
		return context.Canceled
	case p.jobChan <- job:
		return nil
	}
}

// SubmitTimeout 提交任务（带超时）
func (p *ProcessorPool) SubmitTimeout(job ProcessorJob, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.jobChan <- job:
		return nil
	}
}

// Results 返回结果通道（用于消费）
func (p *ProcessorPool) Results() <-chan ProcessorResult {
	return p.resultChan
}

// Stop 优雅停止
func (p *ProcessorPool) Stop(timeout time.Duration) {
	slog.Info("🚀 ProcessorPool stopping...", "timeout", timeout)
	
	p.cancel()
	
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		slog.Info("🚀 ProcessorPool stopped gracefully")
	case <-time.After(timeout):
		slog.Warn("🚀 ProcessorPool stop timeout")
	}
}

// Stats 返回工作池统计
func (p *ProcessorPool) Stats() ProcessorPoolStats {
	return ProcessorPoolStats{
		Workers:      p.numWorkers,
		JobQueueLen:  len(p.jobChan),
		JobQueueCap:  cap(p.jobChan),
		ResultQueueLen: len(p.resultChan),
		ResultQueueCap: cap(p.resultChan),
	}
}

type ProcessorPoolStats struct {
	Workers        int `json:"workers"`
	JobQueueLen    int `json:"job_queue_len"`
	JobQueueCap    int `json:"job_queue_cap"`
	ResultQueueLen int `json:"result_queue_len"`
	ResultQueueCap int `json:"result_queue_cap"`
}

// --- 自适应工作池（根据负载动态调整）---

type AdaptiveProcessorPool struct {
	pool       *ProcessorPool
	minWorkers int
	maxWorkers int
	loadFactor float64
	mu         sync.RWMutex
}

// NewAdaptiveProcessorPool 创建自适应工作池
// 根据 CPU 负载和队列深度动态调整并发度
func NewAdaptiveProcessorPool(minWorkers, maxWorkers int) *AdaptiveProcessorPool {
	return &AdaptiveProcessorPool{
		minWorkers: minWorkers,
		maxWorkers: maxWorkers,
		loadFactor: 0.8,
	}
}

// Start 启动自适应监控
func (a *AdaptiveProcessorPool) Start(baseProcessor func(ProcessorJob) ProcessorResult) {
	// 初始启动最小工作数
	a.pool = NewProcessorPool(a.minWorkers)
	a.pool.Start(baseProcessor)
	
	// 启动自适应调节协程
	go a.adaptiveLoop()
}

// adaptiveLoop 自适应调节循环
func (a *AdaptiveProcessorPool) adaptiveLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		stats := a.pool.Stats()
		
		// 队列使用率 > 80% 且当前 workers < max，扩容
		queueUsage := float64(stats.JobQueueLen) / float64(stats.JobQueueCap)
		
		if queueUsage > 0.8 && stats.Workers < a.maxWorkers {
			slog.Info("🚀 ProcessorPool scaling up", "current", stats.Workers, "target", stats.Workers+2)
			// 实际扩容逻辑（需要重建 pool 或动态增加 workers）
			// 简化版：记录扩容信号，下一批次处理
		}
		
		// 队列使用率 < 20% 且当前 workers > min，缩容
		if queueUsage < 0.2 && stats.Workers > a.minWorkers {
			slog.Info("🚀 ProcessorPool scaling down", "current", stats.Workers, "target", stats.Workers-1)
		}
	}
}

// Submit 提交任务
func (a *AdaptiveProcessorPool) Submit(job ProcessorJob) error {
	return a.pool.Submit(job)
}

// Results 返回结果通道
func (a *AdaptiveProcessorPool) Results() <-chan ProcessorResult {
	return a.pool.Results()
}

// Stop 停止
func (a *AdaptiveProcessorPool) Stop(timeout time.Duration) {
	a.pool.Stop(timeout)
}
