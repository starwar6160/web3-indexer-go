package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/jmoiron/sqlx"
)

// PersistTask 携带需要落盘的原始交易数据
type PersistTask struct {
	Height    uint64            // 区块高度
	Block     models.Block      // 区块元数据
	Transfers []models.Transfer // 提取出的转账记录
	Sequence  uint64            // 消息序列号 (用于追踪)
}

// AsyncWriter 负责异步持久化逻辑
type AsyncWriter struct {
	// 1. 输入通道：海量内存缓冲利用 128G 内存彻底消除背压
	taskChan chan PersistTask

	db            *sqlx.DB
	orchestrator  *Orchestrator
	chainID       int64
	ephemeralMode bool

	// 2. 批处理配置
	batchSize     int
	flushInterval time.Duration

	// 状态控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 性能指标 (原子操作)
	diskWatermark          atomic.Uint64
	writeDuration          atomic.Int64 // 纳秒
	emergencyDrainCooldown atomic.Bool // 🚀 紧急排水冷却标志，防止频繁触发
}
