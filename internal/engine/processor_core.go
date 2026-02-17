package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jmoiron/sqlx"
)

// repositoryAdapter 适配 sqlx.DB 到 DBUpdater 接口
type repositoryAdapter struct {
	db *sqlx.DB
}

func (r *repositoryAdapter) UpdateTokenSymbol(tokenAddress, symbol string) error {
	query := `UPDATE transfers SET symbol = $1 WHERE token_address = $2 AND (symbol IS NULL OR symbol = '')`
	_, err := r.db.Exec(query, symbol, tokenAddress)
	return err
}

func (r *repositoryAdapter) UpdateTokenDecimals(tokenAddress string, decimals uint8) error {
	// 预留方法，当前 schema 没有 decimals 字段
	return nil
}

// TransferEventHash is the ERC20 Transfer event signature hash
// 🚀 工业级修正：0xddf252ad...0afda6
var TransferEventHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f5514cfc0afda6")

// ErrReorgDetected is returned when a blockchain reorganization is detected
var ErrReorgDetected = errors.New("reorg detected: parent hash mismatch")

// ErrReorgNeedRefetch is returned when blocks need to be refetched due to reorg
var ErrReorgNeedRefetch = errors.New("reorg detected: need to refetch from common ancestor")

// ReorgError 携带触发高度的 reorg 错误（用于上层处理）
type ReorgError struct {
	At *big.Int
}

func (e ReorgError) Error() string {
	return fmt.Sprintf("reorg detected at block %s", e.At.String())
}

type Processor struct {
	db               *sqlx.DB
	client           RPCClient // RPC client interface for reorg recovery
	metrics          *Metrics  // Prometheus metrics
	watchedAddresses map[common.Address]bool
	EventHook        func(eventType string, data interface{}) // 实时事件回调

	// DLQ / Retry Queue
	retryQueue chan BlockData
	maxRetries int

	// Batch Checkpoint
	checkpointBatch           int
	blocksSinceLastCheckpoint int

	chainID         int64
	enableSimulator bool
	networkMode     string

	// 🎨 Metadata Enricher (异步元数据解析器)
	enricher *MetadataEnricher
}

func NewProcessor(db *sqlx.DB, client RPCClient, retryQueueSize int, chainID int64, enableSimulator bool, networkMode string) *Processor {
	p := &Processor{
		db:                        db,
		client:                    client,
		metrics:                   GetMetrics(),
		watchedAddresses:          make(map[common.Address]bool),
		retryQueue:                make(chan BlockData, retryQueueSize),
		maxRetries:                3,
		checkpointBatch:           100, // 默认每 100 块持久化一次
		blocksSinceLastCheckpoint: 0,
		chainID:                   chainID,
		enableSimulator:           enableSimulator,
		networkMode:               networkMode,
	}

	// 🎨 初始化元数据丰富器（仅用于生产网络，Anvil 不需要）
	if chainID != 31337 {
		// 从 RPC 池中获取一个客户端用于元数据抓取
		var metadataClient LowLevelRPCClient
		if enhancedPool, ok := client.(*EnhancedRPCClientPool); ok {
			metadataClient = enhancedPool.GetClientForMetadata()
		}
		
		if metadataClient != nil {
			// 使用 Repository 包装 db 以满足 DBUpdater 接口
			repo := &repositoryAdapter{db: db}
			p.enricher = NewMetadataEnricher(metadataClient, repo, Logger)
			Logger.Info("🎨 [Processor] Metadata Enricher initialized", "chain_id", chainID)
		}
	}

	return p
}

// SetBatchCheckpoint 设置检查点持久化批次大小
func (p *Processor) SetBatchCheckpoint(batchSize int) {
	p.checkpointBatch = batchSize
}

// StartRetryWorker 启动异步重试工人
func (p *Processor) StartRetryWorker(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				Logger.Error("processor_retry_worker_panic", "err", r)
			}
		}()
		Logger.Info("processor_retry_worker_started")
		for {
			select {
			case <-ctx.Done():
				Logger.Info("processor_retry_worker_stopping")
				return
			case data := <-p.retryQueue:
				// 简单的指数退避重试逻辑可以在这里扩展
				Logger.Warn("retrying_failed_block_from_queue",
					slog.String("block", data.Number.String()),
				)
				if err := p.ProcessBlockWithRetry(ctx, data, 1); err != nil {
					Logger.Error("block_failed_all_retries_sent_to_dead_letter",
						slog.String("block", data.Number.String()),
						slog.String("error", err.Error()),
					)
					// 这里可以进一步将 data 写入持久化存储（如数据库中的 dead_letter_blocks 表）
				}
			}
		}
	}()
}

// SetWatchedAddresses sets the addresses to monitor
func (p *Processor) SetWatchedAddresses(addresses []string) {
	p.watchedAddresses = make(map[common.Address]bool)
	for _, addr := range addresses {
		p.watchedAddresses[common.HexToAddress(addr)] = true
		Logger.Info("processor_watching_address", slog.String("address", strings.ToLower(addr)))
	}
}

// ProcessBlockWithRetry 带重试的区块处理
func (p *Processor) ProcessBlockWithRetry(ctx context.Context, data BlockData, maxRetries int) error {
	var err error

	for i := 0; i < maxRetries; i++ {
		err = p.ProcessBlock(ctx, data)
		if err == nil {
			return nil
		}

		// 检查是否是致命错误（不需要重试）
		if isFatalError(err) {
			return err
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 指数退避重试：1s, 2s, 4s
		backoff := time.Duration(1<<i) * time.Second
		LogRPCRetry("ProcessBlock", i+1, err)
		select {
		case <-time.After(backoff):
			// 继续重试
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("max retries exceeded for block %s: %w", func() string {
		if data.Block != nil {
			return data.Block.Number().String()
		}
		if data.Number != nil {
			return data.Number.String()
		}
		return "unknown"
	}(), err)
}

// isFatalError 判断错误是否不需要重试
func isFatalError(err error) bool {
	if err == nil {
		return false
	}

	// Reorg 检测错误需要特殊处理，不是简单重试
	if err == ErrReorgDetected {
		return true
	}

	// ReorgError 也是致命错误（需要上层处理）
	if _, ok := err.(ReorgError); ok {
		return true
	}

	// 上下文取消不需要重试
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}

	return false
}
