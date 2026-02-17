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
		if data.Err != nil || data.Block == nil {
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
				activity := p.ProcessLog(vLog)
				if activity != nil {
					validTransfers = append(validTransfers, *activity)
					txWithRealLogs[activity.TxHash] = true
				}
			}
		}

		blockNum := block.Number()
		// B. 扫描交易列表 (捕获部署与原生转账)
		syntheticIdx := uint(20000)
		for _, tx := range block.Transactions() {
			msg, err := types.Sender(types.LatestSignerForChainID(big.NewInt(chainID)), tx)
			fromAddr := "0xunknown"
			if err == nil {
				fromAddr = msg.Hex()
			}

			if tx.To() == nil {
				validTransfers = append(validTransfers, models.Transfer{
					BlockNumber:  models.BigInt{Int: blockNum},
					TxHash:       tx.Hash().Hex(),
					LogIndex:     syntheticIdx,
					From:         strings.ToLower(fromAddr),
					To:           "0xcontract_creation",
					Amount:       models.NewUint256FromBigInt(tx.Value()),
					TokenAddress: "0x0000000000000000000000000000000000000000",
					Symbol:       "EVM",
					Type:         "DEPLOY",
				})
				syntheticIdx++
				continue
			}

			if tx.Value().Cmp(big.NewInt(0)) > 0 && !txWithRealLogs[tx.Hash().Hex()] {
				validTransfers = append(validTransfers, models.Transfer{
					BlockNumber:  models.BigInt{Int: blockNum},
					TxHash:       tx.Hash().Hex(),
					LogIndex:     syntheticIdx,
					From:         strings.ToLower(fromAddr),
					To:           strings.ToLower(tx.To().Hex()),
					Amount:       models.NewUint256FromBigInt(tx.Value()),
					TokenAddress: "0x0000000000000000000000000000000000000000",
					Symbol:       "ETH",
					Type:         "ETH_TRANSFER",
				})
				syntheticIdx++
			}
		}

		// 🚀 模拟模式：强制生成 Synthetic Transfer（让空链也有数据）
		// 记录当前 block 的 transfer 数量（在添加 synthetic 之前）
		transfersBeforeThisBlock := len(validTransfers)
		if p.enableSimulator && p.networkMode == "anvil" {
			Logger.Info("🔍 [ANVIL-BATCH] Checking if synthetic transfer needed",
				slog.String("block", blockNum.String()),
				slog.Int("existing_transfers", transfersBeforeThisBlock),
			)
		}

		// 如果这个区块没有任何 Transfer，生成一个 Synthetic Transfer
		if transfersBeforeThisBlock == 0 && chainID == 31337 && p.enableSimulator {
			// ... (keep existing synthetic logic if still needed, but check p.enableSimulator)
			mockTokens := []struct {
				addr   common.Address
				symbol string
			}{
				{common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), "USDC"},
				{common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), "USDT"},
				{common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), "WBTC"},
				{common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"), "WETH"},
				{common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"), "DAI"},
			}
			selectedToken := mockTokens[int(blockNum.Int64())%len(mockTokens)]

			mockFrom := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" // Anvil Account #0
			mockTo := "0x70997970C51812dc3A010C7d01b50e0d17dc79ee"   // Anvil Account #1
			mockAmount := big.NewInt(int64(blockNum.Int64() % 1000000000)) // 伪随机金额

			anvilTransfer := models.Transfer{
				BlockNumber:  models.BigInt{Int: blockNum},
				TxHash:       common.BytesToHash(append(block.Hash().Bytes(), []byte("ANVIL_MOCK")...)).Hex(),
				LogIndex:     99999, // 特殊标记
				From:         strings.ToLower(mockFrom),
				To:           strings.ToLower(mockTo),
				Amount:       models.NewUint256FromBigInt(mockAmount),
				TokenAddress: strings.ToLower(selectedToken.addr.Hex()), // ✅ 使用真实的代币地址
				Symbol:       selectedToken.symbol,                      // ✅ 添加 Symbol
				Type:         "TRANSFER",
			}
			validTransfers = append(validTransfers, anvilTransfer)

			Logger.Info("🏭 [ANVIL-BATCH] Synthetic Transfer generated",
				slog.String("block", blockNum.String()),
				slog.String("token", selectedToken.symbol), // ✅ 显示 Symbol
				slog.String("from", mockFrom),
				slog.String("to", mockTo),
				slog.String("amount", mockAmount.String()),
			)
		}
	}

	if len(validBlocks) == 0 {
		return nil
	}

	dbTx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer func() {
		if err := dbTx.Rollback(); err != nil && err != sql.ErrTxDone {
			Logger.Warn("batch_rollback_failed", "err", err)
		}
	}()

	inserter := NewBulkInserter(p.db)

	if err := inserter.InsertBlocksBatchTx(ctx, dbTx, validBlocks); err != nil {
		return fmt.Errorf("batch insert blocks failed: %w", err)
	}

	if len(validTransfers) > 0 {
		_, err := dbTx.NamedExecContext(ctx, `
			INSERT INTO transfers
			(block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
			VALUES
			(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
			ON CONFLICT (block_number, log_index) DO NOTHING
		`, validTransfers)
		if err != nil {
			return fmt.Errorf("batch insert activities failed: %w", err)
		}
	}

	// 🚀 防御性检查：查找最后一个有效的 block 更新 checkpoint
	var lastValidBlock *types.Block
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Block != nil {
			lastValidBlock = blocks[i].Block
			break
		}
	}

	if lastValidBlock == nil {
		Logger.Warn("⚠️ [BATCH] No valid blocks found in batch, skipping checkpoint update")
		// 仍然提交事务（如果有数据的话）
		if err := dbTx.Commit(); err != nil {
			return fmt.Errorf("failed to commit batch transaction: %w", err)
		}
		return nil
	}

	lastBlock := lastValidBlock
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
			if data.Err != nil || data.Block == nil {
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
				"symbol":        t.Symbol, // 🎨 添加 Symbol 字段供前端渲染 Token Badge
				"type":          t.Type,   // 🚀 新增：活动类型
				"log_index":     t.LogIndex,
			})
		}
	}

	// Update metrics for the last processed block in the batch
	if p.metrics != nil && len(blocks) > 0 {
		lastData := blocks[len(blocks)-1]
		var bNum *big.Int
		if lastData.Block != nil {
			bNum = lastData.Block.Number()
		} else if lastData.Number != nil {
			bNum = lastData.Number
		}

		if bNum != nil && bNum.IsInt64() {
			p.metrics.UpdateCurrentSyncHeight(bNum.Int64())
		}
	}

	return nil
}
