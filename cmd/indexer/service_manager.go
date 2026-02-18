package main

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/jmoiron/sqlx"
)

// ServiceManager 负责协调所有底层组件
type ServiceManager struct {
	db         *sqlx.DB
	rpcPool    engine.RPCClient
	fetcher    *engine.Fetcher
	Processor  *engine.Processor
	reconciler *engine.Reconciler
	chainID    int64
}

func NewServiceManager(db *sqlx.DB, rpcPool engine.RPCClient, chainID int64, retryQueueSize int, rps, burst, concurrency int, enableSimulator bool, networkMode string, enableRecording bool, recordingPath string) *ServiceManager {
	// ✨ 使用工业级限流器创建 Fetcher
	fetcher := engine.NewFetcherWithLimiter(rpcPool, concurrency, rps, burst)
	processor := engine.NewProcessor(db, rpcPool, retryQueueSize, chainID, enableSimulator, networkMode)

	// 🚀 初始化物理分发 Sink
	if enableRecording && recordingPath != "" {
		if lz4Sink, err := engine.NewLz4Sink(recordingPath); err == nil {
			processor.SetSink(lz4Sink)
			engine.Logger.Info("🎙️ [Recorder] LZ4 Recording ACTIVE", "path", recordingPath)
		} else {
			engine.Logger.Error("failed_to_init_lz4_sink", "err", err)
		}
	}

	reconciler := engine.NewReconciler(db, rpcPool, engine.GetMetrics())

	return &ServiceManager{
		db:         db,
		rpcPool:    rpcPool,
		fetcher:    fetcher,
		Processor:  processor,
		reconciler: reconciler,
		chainID:    chainID,
	}
}

// GetStartBlock 封装自愈逻辑
func (sm *ServiceManager) GetStartBlock(ctx context.Context, forceFrom string, resetDB bool) (*big.Int, error) {
	return getStartBlockFromCheckpoint(ctx, sm.db, sm.rpcPool, sm.chainID, forceFrom, resetDB)
}

// StartTailFollow 启动持续追踪
func (sm *ServiceManager) StartTailFollow(ctx context.Context, startBlock *big.Int) {
	slog.Info("🎬 [StartTailFollow] Function called", "start_block", startBlock.String())

	// 🚀 工业级优化：Gap Check (自动补洞)
	// 检查数据库中已有的最大区块号，看是否与本次 startBlock 存在断层
	var maxInDB int64
	err := sm.db.GetContext(ctx, &maxInDB, "SELECT COALESCE(MAX(number), 0) FROM blocks")
	if err == nil && maxInDB > 0 {
		startNum := startBlock.Int64()
		if startNum > maxInDB+1 {
			gapSize := startNum - (maxInDB + 1)
			engine.Logger.Info("🧩 Gap detected! Initiating catch-up sync",
				"last_in_db", maxInDB,
				"start_at", startNum,
				"gap_blocks", gapSize)

			// 启动后台协程回填 Gap，不阻塞主 Tail 流程
			go func() {
				catchupCtx := context.Background()
				if err := sm.fetcher.Schedule(catchupCtx, big.NewInt(maxInDB+1), big.NewInt(startNum-1)); err != nil {
					engine.Logger.Error("failed_to_schedule_catchup", "err", err)
				}
			}()
		}
	}

	// 启动后台指标上报
	go sm.startMetricsReporter(ctx)
	continuousTailFollow(ctx, sm.fetcher, sm.rpcPool, startBlock)
}

// startMetricsReporter 定期上报系统指标到 Prometheus
func (sm *ServiceManager) startMetricsReporter(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	metrics := engine.GetMetrics()
	metrics.RecordStartTime()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 上报数据库连接池状态
			stats := sm.db.Stats()
			metrics.UpdateDBConnections(stats.OpenConnections)

			// 🚀 存储空间监控
			if free, err := engine.CheckStorageSpace("."); err == nil {
				metrics.UpdateDiskFree(free)
			}
		}
	}
}
