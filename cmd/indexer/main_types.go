package main

import (
	"context"
	"database/sql"
	"sync/atomic"

	"web3-indexer-go/internal/config"

	"github.com/jmoiron/sqlx"
)

// DBWrapper wraps sqlx.DB to match the DBInterface
type DBWrapper struct {
	db *sqlx.DB
}

func (w *DBWrapper) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return w.db.ExecContext(ctx, query, args...)
}

var (
	cfg               *config.Config
	selfHealingEvents atomic.Uint64
	forceFrom         string
	Version           = "v2.2.0-intelligence-engine" // 🚀 工业级版本号
)
