package main

import (
	"context"
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
	processor  *engine.Processor
	reconciler *engine.Reconciler
	chainID    int64
}

func NewServiceManager(db *sqlx.DB, rpcPool engine.RPCClient, chainID int64, retryQueueSize int) *ServiceManager {
	fetcher := engine.NewFetcher(rpcPool, 10) // 默认并发 10
	processor := engine.NewProcessor(db, rpcPool, retryQueueSize, chainID)
	reconciler := engine.NewReconciler(db, rpcPool, engine.GetMetrics())

	return &ServiceManager{
		db:         db,
		rpcPool:    rpcPool,
		fetcher:    fetcher,
		processor:  processor,
		reconciler: reconciler,
		chainID:    chainID,
	}
}

// GetStartBlock 封装自愈逻辑
func (sm *ServiceManager) GetStartBlock(ctx context.Context, forceFrom string) (*big.Int, error) {
	return getStartBlockFromCheckpoint(ctx, sm.db, sm.rpcPool, sm.chainID, forceFrom)
}

// StartTailFollow 启动持续追踪
func (sm *ServiceManager) StartTailFollow(ctx context.Context, startBlock *big.Int) {
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
		}
	}
}
