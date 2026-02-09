package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
)

// TransferEventHash is the ERC20 Transfer event signature hash
var TransferEventHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

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

// Processor 处理区块数据写入，支持批量和单条模式
type Processor struct {
	db      *sqlx.DB
	client  RPCClient // RPC client interface for reorg recovery
	metrics *Metrics  // Prometheus metrics
}

func NewProcessor(db *sqlx.DB, client RPCClient) *Processor {
	return &Processor{db: db, client: client}
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

	return fmt.Errorf("max retries exceeded for block %s: %w", data.Block.Number().String(), err)
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

// ProcessBlock 处理单个区块（必须在顺序保证下调用）
func (p *Processor) ProcessBlock(ctx context.Context, data BlockData) error {
	if data.Err != nil {
		return fmt.Errorf("fetch error: %w", data.Err)
	}

	block := data.Block
	blockNum := block.Number()
	start := time.Now()
	Logger.Debug("processing_block", 
		slog.String("block", blockNum.String()), 
		slog.String("hash", block.Hash().Hex()),
	)

	// 开启事务 (ACID 核心)
	tx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		LogTransactionFailed("begin_transaction", blockNum.String(), err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// 无论成功失败，确保 Rollback (Commit 后 Rollback 无效)
	defer tx.Rollback()

	// 1. Reorg 检测 (Parent Hash Check)
	var lastBlock models.Block
	err = tx.GetContext(ctx, &lastBlock,
		"SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = $1",
		new(big.Int).Sub(blockNum, big.NewInt(1)).String())

	if err == nil {
		// 如果找到了上一个区块，检查 Hash 链
		if lastBlock.Hash != block.ParentHash().Hex() {
			LogReorgDetected(blockNum.String(), lastBlock.Hash, block.ParentHash().Hex())
			// 只返回错误，不在当前事务内删除（避免被 defer tx.Rollback() 回滚）
			// 上层会统一处理回滚与重新调度
			return ReorgError{At: new(big.Int).Set(blockNum)}
		}
	} else if err != sql.ErrNoRows {
		// 数据库查询错误（不是空结果）
		return fmt.Errorf("failed to query parent block: %w", err)
	}
	// 如果是第一个区块或父块不存在（可能是同步开始），继续处理

	// 2. 写入 Block
	_, err = tx.NamedExecContext(ctx, `
		INSERT INTO blocks (number, hash, parent_hash, timestamp)
		VALUES (:number, :hash, :parent_hash, :timestamp)
		ON CONFLICT (number) DO UPDATE SET
			hash = EXCLUDED.hash,
			parent_hash = EXCLUDED.parent_hash,
			timestamp = EXCLUDED.timestamp,
			processed_at = NOW()
	`, models.Block{
		Number:     models.BigInt{Int: blockNum},
		Hash:       block.Hash().Hex(),
		ParentHash: block.ParentHash().Hex(),
		Timestamp:  block.Time(),
	})
	if err != nil {
		LogTransactionFailed("insert_block", blockNum.String(), err)
		return fmt.Errorf("failed to insert block: %w", err)
	}

	// 3. 处理 Transfer 事件（如果日志中有）
	if len(data.Logs) > 0 {
		Logger.Debug("scanning_logs",
			slog.String("block", blockNum.String()),
			slog.Int("logs_count", len(data.Logs)),
		)
	}

	for i, vLog := range data.Logs {
		Logger.Debug("processing_log",
			slog.Int("index", i),
			slog.String("contract", vLog.Address.Hex()),
		)

		transfer := p.ExtractTransfer(vLog)
		if transfer != nil {
			Logger.Info("transfer_found",
				slog.String("from", transfer.From),
				slog.String("to", transfer.To),
				slog.String("amount", transfer.Amount.String()),
			)

			_, err = tx.NamedExecContext(ctx, `
				INSERT INTO transfers 
				(block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
				VALUES 
				(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address)
				ON CONFLICT (block_number, log_index) DO UPDATE SET
					from_address = EXCLUDED.from_address,
					to_address = EXCLUDED.to_address,
					amount = EXCLUDED.amount,
					token_address = EXCLUDED.token_address
			`, transfer)
			if err != nil {
				return fmt.Errorf("failed to insert transfer at block %s: %w", blockNum.String(), err)
			}
			Logger.Debug("transfer_saved",
				slog.String("block", blockNum.String()),
				slog.String("tx_hash", transfer.TxHash),
			)
		}
	}

	// 4. 更新 Checkpoint（在同一事务中保证原子性）
	if err := p.updateCheckpointInTx(ctx, tx, 1, blockNum); err != nil {
		return fmt.Errorf("failed to update checkpoint for block %s: %w", blockNum.String(), err)
	}

	// 5. 提交事务
	if err := tx.Commit(); err != nil {
		LogTransactionFailed("commit_transaction", blockNum.String(), err)
		return fmt.Errorf("failed to commit transaction for block %s: %w", blockNum.String(), err)
	}

	// 记录处理耗时和当前同步高度
	if p.metrics != nil {
		p.metrics.RecordBlockProcessed(time.Since(start))
		// 更新当前同步高度 gauge（用于监控）
		p.metrics.UpdateCurrentSyncHeight(blockNum.Int64())
	}

	return nil
}

// updateCheckpointInTx 在事务内更新 checkpoint（保证原子性）
func (p *Processor) updateCheckpointInTx(ctx context.Context, tx *sqlx.Tx, chainID int64, blockNumber *big.Int) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET 
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()
	`, chainID, blockNumber.String())

	if err != nil {
		return fmt.Errorf("failed to update checkpoint: %w", err)
	}

	return nil
}

// UpdateCheckpoint 更新同步检查点（已废弃，保留用于兼容性）
// 警告：此方法在事务外调用，存在数据不一致风险，建议统一使用事务内更新
func (p *Processor) UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := p.updateCheckpointInTx(ctx, tx, chainID, blockNumber); err != nil {
		return fmt.Errorf("failed to update checkpoint: %w", err)
	}

	return tx.Commit()
}

// ExtractTransfer 从区块日志中提取 ERC20 Transfer 事件
func (p *Processor) ExtractTransfer(vLog types.Log) *models.Transfer {
	// 检查是否为 Transfer 事件 (topic[0])
	if len(vLog.Topics) < 3 || vLog.Topics[0] != TransferEventHash {
		return nil
	}

	from := common.BytesToAddress(vLog.Topics[1].Bytes())
	to := common.BytesToAddress(vLog.Topics[2].Bytes())
	// 使用 uint256 处理金额，保证金融级精度
	amount := models.NewUint256FromBigInt(new(big.Int).SetBytes(vLog.Data))

	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     uint(vLog.Index),
		From:         strings.ToLower(from.Hex()),
		To:           strings.ToLower(to.Hex()),
		Amount:       amount,
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
	}
}

// ProcessBatch 批量处理多个区块（用于历史数据同步优化）
func (p *Processor) ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error {
	if len(blocks) == 0 {
		return nil
	}

	// 收集有效的 blocks 和 transfers
	validBlocks := []models.Block{}
	validTransfers := []models.Transfer{}

	for _, data := range blocks {
		if data.Err != nil {
			continue
		}

		block := data.Block
		validBlocks = append(validBlocks, models.Block{
			Number:     models.BigInt{Int: block.Number()},
			Hash:       block.Hash().Hex(),
			ParentHash: block.ParentHash().Hex(),
			Timestamp:  block.Time(),
		})

		// 处理 transfers
		for _, vLog := range data.Logs {
			transfer := p.ExtractTransfer(vLog)
			if transfer != nil {
				validTransfers = append(validTransfers, *transfer)
			}
		}
	}

	if len(validBlocks) == 0 {
		return nil
	}

	tx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer tx.Rollback()

	inserter := NewBulkInserter(p.db)

	if err := inserter.InsertBlocksBatchTx(ctx, tx, validBlocks); err != nil {
		return fmt.Errorf("batch insert blocks failed: %w", err)
	}

	if len(validTransfers) > 0 {
		if err := inserter.InsertTransfersBatchTx(ctx, tx, validTransfers); err != nil {
			return fmt.Errorf("batch insert transfers failed: %w", err)
		}
	}

	lastBlock := blocks[len(blocks)-1].Block
	if err := p.updateCheckpointInTx(ctx, tx, chainID, lastBlock.Number()); err != nil {
		return fmt.Errorf("batch checkpoint update failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	return nil
}

// FindCommonAncestor 递归查找共同祖先（处理深度重组）
// 返回共同祖先的区块号和哈希，以及需要删除的区块列表
func (p *Processor) FindCommonAncestor(ctx context.Context, blockNum *big.Int) (*big.Int, string, []*big.Int, error) {
	Logger.Info("finding_common_ancestor", slog.String("from_block", blockNum.String()))

	toDelete := []*big.Int{}
	currentNum := new(big.Int).Set(blockNum)
	maxLookback := big.NewInt(1000) // 最大回退1000个块防止无限循环

	for currentNum.Cmp(big.NewInt(0)) > 0 && new(big.Int).Sub(blockNum, currentNum).Cmp(maxLookback) <= 0 {
		// 从RPC获取链上区块
		rpcBlock, err := p.client.BlockByNumber(ctx, currentNum)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to get block %s from RPC: %w", currentNum.String(), err)
		}

		// 查询本地数据库中相同高度的区块
		var localBlock models.Block
		err = p.db.GetContext(ctx, &localBlock,
			"SELECT hash FROM blocks WHERE number = $1", currentNum.String())

		if err == sql.ErrNoRows {
			// 本地没有这个区块，继续往前找
			toDelete = append(toDelete, new(big.Int).Set(currentNum))
			currentNum.Sub(currentNum, big.NewInt(1))
			continue
		}
		if err != nil {
			return nil, "", nil, fmt.Errorf("database error at block %s: %w", currentNum.String(), err)
		}

		// 检查哈希是否匹配
		if strings.ToLower(localBlock.Hash) == strings.ToLower(rpcBlock.Hash().Hex()) {
			// 找到共同祖先！
			Logger.Info("common_ancestor_found",
				slog.String("block", currentNum.String()),
				slog.String("hash", localBlock.Hash),
			)
			return currentNum, localBlock.Hash, toDelete, nil
		}

		// 哈希不匹配，这个区块也在重组链上，需要删除
		toDelete = append(toDelete, new(big.Int).Set(currentNum))

		// 继续查找父区块（使用RPC返回的parent hash）
		parentNum := new(big.Int).Sub(currentNum, big.NewInt(1))
		currentNum.Set(parentNum)
	}

	return nil, "", nil, fmt.Errorf("common ancestor not found within %s blocks", maxLookback.String())
}

// HandleDeepReorg 处理深度重组（超过1个块的重组）
// 调用此函数前必须停止Fetcher并清空其队列
func (p *Processor) HandleDeepReorg(ctx context.Context, blockNum *big.Int) (*big.Int, error) {
	// 查找共同祖先
	ancestorNum, _, toDelete, err := p.FindCommonAncestor(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to find common ancestor: %w", err)
	}

	LogReorgHandled(len(toDelete), ancestorNum.String())

	// 在单个事务内执行回滚（保证原子性）
	tx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("failed to begin reorg transaction: %w", err)
	}
	defer tx.Rollback()

	// 批量删除所有分叉区块（cascade 会自动删除 transfers）
	if len(toDelete) > 0 {
		// 找到最小的要删除的块号
		minDelete := toDelete[0]
		for _, num := range toDelete {
			if num.Cmp(minDelete) < 0 {
				minDelete = num
			}
		}
		// 删除所有 >= minDelete 的块（更高效）
		_, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE number >= $1", minDelete.String())
		if err != nil {
			return nil, fmt.Errorf("failed to delete reorg blocks: %w", err)
		}
	}

	// 更新 checkpoint 回退到祖先高度
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET 
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()
	`, 1, ancestorNum.String())
	if err != nil {
		return nil, fmt.Errorf("failed to update checkpoint during reorg: %w", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit reorg transaction: %w", err)
	}

	Logger.Info("deep_reorg_handled",
		slog.String("resume_block", new(big.Int).Add(ancestorNum, big.NewInt(1)).String()),
	)

	return ancestorNum, nil
}
