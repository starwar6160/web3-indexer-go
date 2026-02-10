package main

import (
	"context"
	"math/big"

	"web3-indexer-go/internal/engine"
	"github.com/jmoiron/sqlx"
)

// ServiceManager 负责协调所有底层组件
type ServiceManager struct {
	db         *sqlx.DB
	rpcPool    *engine.RPCClientPool
	fetcher    *engine.Fetcher
	processor  *engine.Processor
	reconciler *engine.Reconciler
	chainID    int64
}

func NewServiceManager(db *sqlx.DB, rpcPool *engine.RPCClientPool, chainID int64, retryQueueSize int) *ServiceManager {
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
func (sm *ServiceManager) GetStartBlock(ctx context.Context) (*big.Int, error) {
	return getStartBlockFromCheckpoint(ctx, sm.db, sm.rpcPool, sm.chainID)
}

// StartTailFollow 启动持续追踪
func (sm *ServiceManager) StartTailFollow(ctx context.Context, startBlock *big.Int) {
	continuousTailFollow(ctx, sm.fetcher, sm.rpcPool, startBlock)
}
