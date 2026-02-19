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

	"web3-indexer-go/internal/models"

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

func (r *repositoryAdapter) UpdateTokenDecimals(_ string, _ uint8) error {
	// 预留方法，当前 schema 没有 decimals 字段
	return nil
}

func (r *repositoryAdapter) SaveTokenMetadata(meta models.TokenMetadata, address string) error {
	query := `
		INSERT INTO token_metadata (address, symbol, decimals, name, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (address) DO UPDATE SET
			symbol = EXCLUDED.symbol,
			decimals = EXCLUDED.decimals,
			name = EXCLUDED.name,
			updated_at = NOW()
	`
	_, err := r.db.Exec(query, strings.ToLower(address), meta.Symbol, meta.Decimals, meta.Name)
	return err
}

func (r *repositoryAdapter) LoadAllMetadata() (map[string]models.TokenMetadata, error) {
	var rows []struct {
		Address  string `db:"address"`
		Symbol   string `db:"symbol"`
		Decimals uint8  `db:"decimals"`
		Name     string `db:"name"`
	}

	err := r.db.Select(&rows, "SELECT address, symbol, decimals, name FROM token_metadata")
	if err != nil {
		return nil, err
	}

	result := make(map[string]models.TokenMetadata)
	for _, row := range rows {
		result[strings.ToLower(row.Address)] = models.TokenMetadata{
			Symbol:   row.Symbol,
			Decimals: row.Decimals,
			Name:     row.Name,
		}
	}
	return result, nil
}

func (r *repositoryAdapter) GetMaxStoredBlock(ctx context.Context) (int64, error) {
	var dbMax int64
	err := r.db.GetContext(ctx, &dbMax, "SELECT COALESCE(MAX(number), 0) FROM blocks")
	return dbMax, err
}

func (r *repositoryAdapter) GetSyncCursor(ctx context.Context) (int64, error) {
	var cursor string
	err := r.db.GetContext(ctx, &cursor, "SELECT COALESCE(last_synced_block, '0') FROM sync_checkpoints LIMIT 1")
	if err != nil {
		return 0, err
	}
	num, _ := new(big.Int).SetString(cursor, 10)
	if num == nil {
		return 0, nil
	}
	return num.Int64(), nil
}

func (r *repositoryAdapter) PruneFutureData(ctx context.Context, chainHead int64) error {
	// Directly execute delete queries
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck // Rollback is standard practice, error is usually non-critical during cleanup

	headStr := fmt.Sprintf("%d", chainHead)

	if _, err := tx.ExecContext(ctx, "DELETE FROM transfers WHERE block_number > $1", headStr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE number > $1", headStr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1, updated_at = NOW()", headStr); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *repositoryAdapter) UpdateSyncCursor(ctx context.Context, height int64) error {
	headStr := fmt.Sprintf("%d", height)
	if _, err := r.db.ExecContext(ctx, "UPDATE sync_checkpoints SET last_synced_block = $1, updated_at = NOW()", headStr); err != nil {
		return err
	}
	return nil
}

// TransferEventHash defined in signatures.go

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

	// 🚀 HotBuffer (内存热数据池)
	hotBuffer *HotBuffer

	// 🚀 DataSink (多路分发支持)
	sink DataSink

	// 🚀 Reorg 检测缓存：避免每块都查 DB
	lastBlockHashMu sync.Mutex
	lastBlockNum    int64
	lastBlockHash   string
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
		hotBuffer:                 NewHotBuffer(50000), // 默认 5 万条热数据
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

// GetDB returns the underlying sqlx.DB instance
func (p *Processor) GetDB() *sqlx.DB {
	return p.db
}

// GetRPCClient returns the RPC client used by the processor
func (p *Processor) GetRPCClient() RPCClient {
	return p.client
}

// GetSymbol returns the token symbol, triggering enrichment if missing
func (p *Processor) GetSymbol(addr common.Address) string {
	if p.enricher != nil {
		return p.enricher.GetSymbol(addr)
	}
	return addr.Hex()[:10] + "..."
}

// GetRepoAdapter returns the underlying repository adapter for the guard
func (p *Processor) GetRepoAdapter() DBUpdater {
	return &repositoryAdapter{db: p.db}
}

// GetHotBuffer returns the HotBuffer instance
func (p *Processor) GetHotBuffer() *HotBuffer {
	return p.hotBuffer
}

// SetSink sets the data sink for the processor
func (p *Processor) SetSink(sink DataSink) {
	p.sink = sink
}

// GetSink returns the current data sink
func (p *Processor) GetSink() DataSink {
	return p.sink
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
		return systemStateUnknownStr
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
