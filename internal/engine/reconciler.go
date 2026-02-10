package engine

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"github.com/jmoiron/sqlx"
)

// Reconciler 负责对已索引数据进行终态一致性审计
type Reconciler struct {
	db      *sqlx.DB
	rpcPool RPCClient
	metrics *Metrics
	logger  *slog.Logger
}

func NewReconciler(db *sqlx.DB, rpcPool RPCClient, metrics *Metrics) *Reconciler {
	return &Reconciler{
		db:      db,
		rpcPool: rpcPool,
		metrics: metrics,
		logger:  Logger,
	}
}

// StartPeriodicAudit 启动定期后台审计
func (r *Reconciler) StartPeriodicAudit(ctx context.Context, interval time.Duration, lookback int64) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	r.logger.Info("auditor_worker_started", slog.Duration("interval", interval), slog.Int64("lookback", lookback))

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("auditor_worker_stopping")
			return
		case <-ticker.C:
			r.performAudit(ctx, lookback)
		}
	}
}

func (r *Reconciler) performAudit(ctx context.Context, lookback int64) {
	// 1. 获取本地最高块号
	var maxNum int64
	err := r.db.GetContext(ctx, &maxNum, "SELECT COALESCE(MAX(number), 0) FROM blocks")
	if err != nil || maxNum == 0 {
		return
	}

	start := maxNum - lookback
	if start < 0 {
		start = 0
	}

	r.logger.Debug("audit_cycle_starting", slog.Int64("from", start), slog.Int64("to", maxNum))

	// 2. 抽样检查（随机抽取 5 个区块进行深度哈希对齐）
	// 在工业级场景中，我们不全量对齐（太慢），而是通过概率抽样保证整体一致性
	for i := 0; i < 5; i++ {
		// 这里简单演示：抽查最近的块
		checkNum := maxNum - int64(i*10)
		if checkNum < start {
			break
		}

		r.auditBlock(ctx, big.NewInt(checkNum))
	}
}

func (r *Reconciler) auditBlock(ctx context.Context, number *big.Int) {
	// 获取 RPC 哈希
	rpcBlock, err := r.rpcPool.BlockByNumber(ctx, number)
	if err != nil {
		return
	}

	// 获取 DB 哈希
	var dbHash string
	err = r.db.GetContext(ctx, &dbHash, "SELECT hash FROM blocks WHERE number = $1", number.String())
	if err != nil {
		r.logger.Error("🚨 AUDIT_DATA_MISSING", slog.String("block", number.String()))
		return
	}

	// 核心对齐
	if rpcBlock.Hash().Hex() != dbHash {
		r.logger.Error("🚨 AUDIT_HASH_MISMATCH",
			slog.String("block", number.String()),
			slog.String("rpc", rpcBlock.Hash().Hex()),
			slog.String("db", dbHash),
		)
		// 可以在此触发 HandleDeepReorg 逻辑进行自动修复
	} else {
		r.logger.Debug("audit_passed", slog.String("block", number.String()))
	}
}
