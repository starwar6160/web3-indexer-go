package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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

// Processor 处理区块数据写入，支持批量和单条模式
type Processor struct {
	db *sqlx.DB
}

func NewProcessor(db *sqlx.DB) *Processor {
	return &Processor{db: db}
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
		
		// 指数退避重试
		backoff := time.Duration(i+1) * time.Second
		log.Printf("Retry %d/%d for block %s after %v: %v", i+1, maxRetries, data.Block.Number().String(), backoff, err)
		time.Sleep(backoff)
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
	log.Printf("Processing block: %s | Hash: %s", blockNum.String(), block.Hash().Hex())

	// 开启事务 (ACID 核心)
	tx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
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
			log.Printf("🚨 REORG DETECTED at block %s! Expected parent %s, got %s", 
				blockNum.String(), lastBlock.Hash, block.ParentHash().Hex())
			
			// 触发回滚逻辑
			_, err = tx.ExecContext(ctx, 
				"DELETE FROM blocks WHERE number >= $1", 
				new(big.Int).Sub(blockNum, big.NewInt(1)).String())
			if err != nil {
				return fmt.Errorf("reorg rollback failed: %w", err)
			}
			
			return ErrReorgDetected
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
		return fmt.Errorf("failed to insert block: %w", err)
	}

	// 3. 处理 Transfer 事件（如果日志中有）
	for _, vLog := range data.Logs {
		transfer := p.ExtractTransfer(vLog)
		if transfer != nil {
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
		}
	}

	// 4. 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for block %s: %w", blockNum.String(), err)
	}
	
	return nil
}

// UpdateCheckpoint 更新同步检查点（在Sequencer确认顺序后调用）
func (p *Processor) UpdateCheckpoint(ctx context.Context, chainID int64, blockNumber *big.Int) error {
	_, err := p.db.ExecContext(ctx, `
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

// ExtractTransfer 从区块日志中提取 ERC20 Transfer 事件
func (p *Processor) ExtractTransfer(vLog types.Log) *models.Transfer {
	// 检查是否为 Transfer 事件 (topic[0])
	if len(vLog.Topics) < 3 || vLog.Topics[0] != TransferEventHash {
		return nil
	}

	from := common.BytesToAddress(vLog.Topics[1].Bytes())
	to := common.BytesToAddress(vLog.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(vLog.Data)

	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     uint(vLog.Index),
		From:         strings.ToLower(from.Hex()),
		To:           strings.ToLower(to.Hex()),
		Amount:       models.BigInt{Int: amount},
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
	}
}

// ProcessBatch 批量处理多个区块（用于历史数据同步优化）
func (p *Processor) ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error {
	if len(blocks) == 0 {
		return nil
	}
	
	// 开启事务
	tx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer tx.Rollback()
	
	// 批量插入 blocks
	blockQuery := `
		INSERT INTO blocks (number, hash, parent_hash, timestamp)
		VALUES (:number, :hash, :parent_hash, :timestamp)
		ON CONFLICT (number) DO UPDATE SET
			hash = EXCLUDED.hash,
			parent_hash = EXCLUDED.parent_hash,
			timestamp = EXCLUDED.timestamp,
			processed_at = NOW()
	`
	
	for _, data := range blocks {
		if data.Err != nil {
			continue
		}
		
		block := data.Block
		_, err = tx.NamedExecContext(ctx, blockQuery, models.Block{
			Number:     models.BigInt{Int: block.Number()},
			Hash:       block.Hash().Hex(),
			ParentHash: block.ParentHash().Hex(),
			Timestamp:  block.Time(),
		})
		if err != nil {
			return fmt.Errorf("batch insert block %s failed: %w", block.Number().String(), err)
		}
		
		// 处理 transfers
		for _, vLog := range data.Logs {
			transfer := p.ExtractTransfer(vLog)
			if transfer != nil {
				_, err = tx.NamedExecContext(ctx, `
					INSERT INTO transfers 
					(block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
					VALUES 
					(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address)
					ON CONFLICT (block_number, log_index) DO NOTHING
				`, transfer)
				if err != nil {
					return fmt.Errorf("batch insert transfer failed: %w", err)
				}
			}
		}
	}
	
	// 更新 checkpoint 到最后一个区块
	lastBlock := blocks[len(blocks)-1].Block
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET 
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()
	`, chainID, lastBlock.Number().String())
	if err != nil {
		return fmt.Errorf("batch checkpoint update failed: %w", err)
	}
	
	return tx.Commit()
}
