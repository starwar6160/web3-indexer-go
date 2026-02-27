package engine

import (
	"context"
	"log/slog"
	"math"
	"math/big"
	"strings"
	"time"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const networkAnvil = "anvil"

// ProcessBatch 批量处理多个区块 (横滨实验室异步落盘版)
func (p *Processor) ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error {
	if len(blocks) == 0 {
		return nil
	}

	for _, data := range blocks {
		if data.Err != nil || data.Block == nil {
			continue
		}

		block := data.Block
		blockNum := block.Number()

		// 1. 提取活动 (内存提取)
		txWithRealLogs := make(map[string]bool)
		activities := []models.Transfer{}

		// 提取 Logs
		for _, vLog := range data.Logs {
			activity := p.ProcessLog(vLog)
			if activity != nil {
				activities = append(activities, *activity)
				txWithRealLogs[activity.TxHash] = true
			}
		}

		// 提取 Transactions (Deploy, ETH transfer, etc.)
		p.processBatchTransactions(block, chainID, txWithRealLogs, &activities)

		// Anvil 模拟数据
		p.processBatchSynthetic(block, chainID, &activities)

		// 2. 构建 PersistTask
		var baseFee *models.BigInt
		if block.BaseFee() != nil {
			baseFee = &models.BigInt{Int: block.BaseFee()}
		}
		mBlock := models.Block{
			Number:           models.BigInt{Int: block.Number()},
			Hash:             block.Hash().Hex(),
			ParentHash:       block.ParentHash().Hex(),
			Timestamp:        block.Time(),
			GasLimit:         block.GasLimit(),
			GasUsed:          block.GasUsed(),
			BaseFeePerGas:    baseFee,
			TransactionCount: len(block.Transactions()),
		}

		task := PersistTask{
			Height:    blockNum.Uint64(),
			Block:     mBlock,
			Transfers: activities,
			Sequence:  uint64(time.Now().UnixNano()) & uint64(math.MaxInt64),
		}

		// 3. 核心分发 (SSOT)
		GetOrchestrator().Dispatch(CmdCommitBatch, task)

		// 4. 事件推送 (UI 即时响应)
		p.pushEvents(block, activities, nil)
	}

	p.updateBatchMetrics(blocks)

	return nil
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

		// 🎭 Dynamic Entities: Cycle through different Anvil accounts
		anvilAccounts := []string{
			"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", // Account #0
			"0x70997970C51812dc3A010C7d01b50e0d17dc79ee", // Account #1
			"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", // Account #2
			"0x90F79bf6EB2c4f870365E785982E1f101E93b906", // Account #3
			"0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65", // Account #4
		}

		fromIdx := blockNum.Uint64() % uint64(len(anvilAccounts))
		toIdx := (blockNum.Uint64() + 1) % uint64(len(anvilAccounts))
		mockFrom := anvilAccounts[fromIdx]
		mockTo := anvilAccounts[toIdx]

		// 💰 Varied Amounts: Multi-scale amounts for realistic visual variety
		baseAmount := blockNum.Int64() % 1000
		multiplier := int64(1)
		switch blockNum.Uint64() % 4 {
		case 0:
			multiplier = 1000000 // 1M
		case 1:
			multiplier = 1000000000 // 1B
		case 2:
			multiplier = 1 // Units
		}
		mockAmount := big.NewInt(baseAmount * multiplier)

		// 🎨 Diversity: Randomize Activity Types for UI richness
		activityTypes := []string{"TRANSFER", "SWAP", "MINT", "FAUCET_CLAIM", "APPROVE", "LIQUIDITY", "BURN"}
		selectedType := activityTypes[blockNum.Uint64()%uint64(len(activityTypes))]

		anvilTransfer := models.Transfer{
			BlockNumber:  models.BigInt{Int: blockNum},
			TxHash:       common.BytesToHash(append(block.Hash().Bytes(), []byte("ANVIL_MOCK")...)).Hex(),
			LogIndex:     99999,
			From:         strings.ToLower(mockFrom),
			To:           strings.ToLower(mockTo),
			Amount:       models.NewUint256FromBigInt(mockAmount),
			TokenAddress: strings.ToLower(selectedToken.addr.Hex()),
			Symbol:       selectedToken.symbol,
			Type:         selectedType,
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

	if bNum != nil {
		// 🚀 G115 安全转换
		if bNum.IsInt64() {
			p.metrics.UpdateCurrentSyncHeight(bNum.Int64())
		} else {
			// 防御性截断，确保指标系统不会因为大高度而崩溃
			p.metrics.UpdateCurrentSyncHeight(SafeUint64ToInt64(bNum.Uint64() & uint64(math.MaxInt64)))
		}
	}
}
