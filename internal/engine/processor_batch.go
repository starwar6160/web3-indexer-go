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
	"github.com/jmoiron/sqlx"
)

const networkAnvil = "anvil"

// ProcessBatch 批量处理多个区块（用于历史数据同步优化）
func (p *Processor) ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error {
	if len(blocks) == 0 {
		return nil
	}

	// 收集有效的 blocks and transfers
	validBlocks := make([]models.Block, 0, len(blocks))
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

		// 处理该区块的活动
		txWithRealLogs := p.processBatchLogs(data, &validTransfers)
		p.processBatchTransactions(block, chainID, txWithRealLogs, &validTransfers)
		p.processBatchSynthetic(block, chainID, &validTransfers)
	}

	if len(validBlocks) == 0 {
		return nil
	}

	dbTx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer func() {
		if rollbackErr := dbTx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			Logger.Warn("batch_rollback_failed", "err", rollbackErr)
		}
	}()

	if err := p.insertBatchData(ctx, dbTx, validBlocks, validTransfers); err != nil {
		return err
	}

	// 🚀 防御性检查：查找最后一个有效的 block 更新 checkpoint
	var lastValidBlock *types.Block
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Block != nil {
			lastValidBlock = blocks[i].Block
			break
		}
	}

	if lastValidBlock != nil {
		if err := p.updateCheckpointInTx(ctx, dbTx, chainID, lastValidBlock.Number()); err != nil {
			return fmt.Errorf("batch checkpoint update failed: %w", err)
		}
		p.blocksSinceLastCheckpoint = 0
	}

	if err := dbTx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	// 🚀 物理直连：将处理完的转账数据灌入内存热池
	if p.hotBuffer != nil {
		for _, t := range validTransfers {
			p.hotBuffer.Add(t)
		}
	}

	// 🚀 物理分发：如果配置了 DataSink (如 LZ4 录制), 则进行分发
	if p.sink != nil {
		_ = p.sink.WriteBlocks(ctx, validBlocks) // nolint:errcheck // secondary sink failure shouldn't block main flow
		if len(validTransfers) > 0 {
			_ = p.sink.WriteTransfers(ctx, validTransfers) // nolint:errcheck // secondary sink failure shouldn't block main flow
		}
	}

	p.broadcastBatchEvents(blocks, validTransfers)
	p.updateBatchMetrics(blocks)

	return nil
}

func (p *Processor) processBatchLogs(data BlockData, validTransfers *[]models.Transfer) map[string]bool {
	txWithRealLogs := make(map[string]bool)
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
				*validTransfers = append(*validTransfers, *activity)
				txWithRealLogs[activity.TxHash] = true
			}
		}
	}
	return txWithRealLogs
}

func (p *Processor) processBatchTransactions(block *types.Block, chainID int64, txWithRealLogs map[string]bool, validTransfers *[]models.Transfer) {
	blockNum := block.Number()
	syntheticIdx := uint(20000)
	for _, tx := range block.Transactions() {
		msg, err := types.Sender(types.LatestSignerForChainID(big.NewInt(chainID)), tx)
		fromAddr := "0xunknown"
		if err == nil {
			fromAddr = msg.Hex()
		}

		if tx.To() == nil {
			*validTransfers = append(*validTransfers, models.Transfer{
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
			*validTransfers = append(*validTransfers, models.Transfer{
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
}

func (p *Processor) processBatchSynthetic(block *types.Block, chainID int64, validTransfers *[]models.Transfer) {
	blockNum := block.Number()
	transfersBeforeThisBlock := len(*validTransfers)
	if p.enableSimulator && p.networkMode == networkAnvil {
		Logger.Info("🔍 [ANVIL-BATCH] Checking if synthetic transfer needed",
			slog.String("block", blockNum.String()),
			slog.Int("existing_transfers", transfersBeforeThisBlock),
		)
	}

	if transfersBeforeThisBlock == 0 && chainID == 31337 && p.enableSimulator {
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
		selectedToken := mockTokens[blockNum.Uint64()%uint64(len(mockTokens))]

		mockFrom := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" // Anvil Account #0
		mockTo := "0x70997970C51812dc3A010C7d01b50e0d17dc79ee"   // Anvil Account #1
		// #nosec G115 - block number is safe in this context
		mockAmount := big.NewInt(blockNum.Int64() % 1000000000)

		anvilTransfer := models.Transfer{
			BlockNumber:  models.BigInt{Int: blockNum},
			TxHash:       common.BytesToHash(append(block.Hash().Bytes(), []byte("ANVIL_MOCK")...)).Hex(),
			LogIndex:     99999,
			From:         strings.ToLower(mockFrom),
			To:           strings.ToLower(mockTo),
			Amount:       models.NewUint256FromBigInt(mockAmount),
			TokenAddress: strings.ToLower(selectedToken.addr.Hex()),
			Symbol:       selectedToken.symbol,
			Type:         "TRANSFER",
		}
		*validTransfers = append(*validTransfers, anvilTransfer)

		Logger.Info("🏭 [ANVIL-BATCH] Synthetic Transfer generated",
			slog.String("block", blockNum.String()),
			slog.String("token", selectedToken.symbol),
			slog.String("from", mockFrom),
			slog.String("to", mockTo),
			slog.String("amount", mockAmount.String()),
		)
	}
}

func (p *Processor) insertBatchData(ctx context.Context, dbTx *sqlx.Tx, blocks []models.Block, transfers []models.Transfer) error {
	inserter := NewBulkInserter(p.db)
	if err := inserter.InsertBlocksBatchTx(ctx, dbTx, blocks); err != nil {
		return fmt.Errorf("batch insert blocks failed: %w", err)
	}

	if len(transfers) > 0 {
		_, err := dbTx.NamedExecContext(ctx, `
			INSERT INTO transfers
			(block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
			VALUES
			(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
			ON CONFLICT (block_number, log_index) DO NOTHING
		`, transfers)
		if err != nil {
			return fmt.Errorf("batch insert activities failed: %w", err)
		}
	}
	return nil
}

func (p *Processor) broadcastBatchEvents(blocks []BlockData, transfers []models.Transfer) {
	if p.EventHook == nil {
		return
	}
	for _, data := range blocks {
		if data.Block == nil {
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
	for _, t := range transfers {
		p.EventHook("transfer", map[string]interface{}{
			"tx_hash":       t.TxHash,
			"from":          t.From,
			"to":            t.To,
			"value":         t.Amount.String(),
			"block_number":  t.BlockNumber.String(),
			"token_address": t.TokenAddress,
			"symbol":        t.Symbol,
			"type":          t.Type,
			"log_index":     t.LogIndex,
		})
	}
}

func (p *Processor) updateBatchMetrics(blocks []BlockData) {
	if p.metrics == nil || len(blocks) == 0 {
		return
	}
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
