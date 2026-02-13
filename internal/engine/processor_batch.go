package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ProcessBatch 批量处理多个区块（用于历史数据同步优化）
func (p *Processor) ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error {
	if len(blocks) == 0 {
		return nil
	}

	// 收集有效的 blocks and transfers
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

		// 处理 transfers（与 ProcessBlock 相同的地址匹配逻辑）
		txWithRealLogs := make(map[string]bool) // track tx hashes that produced real Transfer logs
		for _, vLog := range data.Logs {
			if len(vLog.Topics) == 0 {
				continue
			}
			logAddrLow := strings.ToLower(vLog.Address.Hex())
			isMatched := false
			for addr := range p.watchedAddresses {
				if strings.ToLower(addr.Hex()) == logAddrLow {
					isMatched = true
					break
				}
			}
			if isMatched || len(p.watchedAddresses) == 0 {
				if vLog.Topics[0] == TransferEventHash {
					transfer := p.ExtractTransfer(vLog)
					if transfer != nil {
						validTransfers = append(validTransfers, *transfer)
						txWithRealLogs[transfer.TxHash] = true
					}
				}
			}
		}

		// Fallback: Scan transactions for direct calls to watched addresses (only if no real log found)
		blockNum := block.Number()
		syntheticIdx := 10000 // high base to avoid conflict with real log_index
		for _, tx := range block.Transactions() {
			if tx.To() != nil {
				txToLow := strings.ToLower(tx.To().Hex())
				isMatched := false
				for addr := range p.watchedAddresses {
					if strings.ToLower(addr.Hex()) == txToLow {
						isMatched = true
						break
					}
				}
				if len(p.watchedAddresses) == 0 {
					isMatched = true
				}
				if isMatched && !txWithRealLogs[tx.Hash().Hex()] {
					Logger.Info("🎯 [Batch] 发现直接调用监控合约的交易（无真实日志，使用合成）",
						slog.String("tx_hash", tx.Hash().Hex()),
						slog.String("to", txToLow),
						slog.String("block", blockNum.String()),
					)
					// 尝试从 Data 中提取金额和接收者
					input := tx.Data()
					syntheticAmount := big.NewInt(1000)
					syntheticTo := txToLow
					if len(input) >= 68 {
						syntheticTo = common.BytesToAddress(input[16:36]).Hex()
						syntheticAmount = new(big.Int).SetBytes(input[len(input)-32:])
					}

					// 尝试获取发送者
					fromAddr := "0xunknown"
					signer := types.LatestSignerForChainID(big.NewInt(chainID))
					if sender, err := types.Sender(signer, tx); err == nil {
						fromAddr = sender.Hex()
					}

					syntheticTransfer := models.Transfer{
						BlockNumber:  models.BigInt{Int: blockNum},
						TxHash:       tx.Hash().Hex(),
						LogIndex:     uint(syntheticIdx),
						From:         strings.ToLower(fromAddr),
						To:           strings.ToLower(syntheticTo),
						Amount:       models.NewUint256FromBigInt(syntheticAmount),
						TokenAddress: txToLow,
					}
					validTransfers = append(validTransfers, syntheticTransfer)
					syntheticIdx++
				}
			}
		}
	}

	if len(validBlocks) == 0 {
		return nil
	}

	dbTx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer dbTx.Rollback()

	inserter := NewBulkInserter(p.db)

	if err := inserter.InsertBlocksBatchTx(ctx, dbTx, validBlocks); err != nil {
		return fmt.Errorf("batch insert blocks failed: %w", err)
	}

	if len(validTransfers) > 0 {
		if err := inserter.InsertTransfersBatchTx(ctx, dbTx, validTransfers); err != nil {
			return fmt.Errorf("batch insert transfers failed: %w", err)
		}
	}

	lastBlock := blocks[len(blocks)-1].Block
	if err := p.updateCheckpointInTx(ctx, dbTx, chainID, lastBlock.Number()); err != nil {
		return fmt.Errorf("batch checkpoint update failed: %w", err)
	}
	p.blocksSinceLastCheckpoint = 0 // 重置计数器

	if err := dbTx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	// 6. 实时事件推送 (在事务成功后)
	if p.EventHook != nil {
		for _, data := range blocks {
			if data.Err != nil {
				continue
			}
			block := data.Block
			p.EventHook("block", map[string]interface{}{
				"number":    block.NumberU64(),
				"hash":      block.Hash().Hex(),
				"timestamp": block.Time(),
				"tx_count":  len(block.Transactions()),
			})
		}
		for _, t := range validTransfers {
			p.EventHook("transfer", map[string]interface{}{
				"tx_hash":       t.TxHash,
				"from":          t.From,
				"to":            t.To,
				"value":         t.Amount.String(),
				"block_number":  t.BlockNumber.String(),
				"token_address": t.TokenAddress,
				"log_index":     t.LogIndex,
			})
		}
	}

	// Update metrics for the last processed block in the batch
	if p.metrics != nil && len(blocks) > 0 {
		lastBlock := blocks[len(blocks)-1].Block
		if lastBlock != nil {
			blockNum := lastBlock.Number()
			if blockNum.IsInt64() {
				p.metrics.UpdateCurrentSyncHeight(blockNum.Int64())
			}
		}
	}

	return nil
}