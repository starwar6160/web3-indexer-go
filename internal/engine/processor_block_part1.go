package engine

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ProcessBlock 处理单个区块（由 Sequencer 顺序调用）
func (p *Processor) ProcessBlock(ctx context.Context, data BlockData) error {
	if data.Err != nil {
		return fmt.Errorf("fetch error: %w", data.Err)
	}

	block := data.Block
	blockNum := block.Number()
	start := time.Now()

	// 1. 🔥 物理对齐：Reorg 检测 (只读 DB 检测)
	if err := p.handleReorgReadOnly(ctx, blockNum, block.ParentHash()); err != nil {
		return err
	}

	// 2. 🔥 逻辑转换：提取所有活动 (不写库)
	activities := p.extractActivities(ctx, blockNum, data.Logs, block.Transactions())

	// 🚀 模拟模式：强制生成 Synthetic Transfer（让空链也有数据）
	activities = p.processAnvilSyntheticNoDB(ctx, blockNum, block, activities)

	// 3. 🔥 物理准备：构建 PersistTask
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
		Sequence:  uint64(time.Now().UnixNano()),
	}

	// 4. 🔥 核心调度：通过 Orchestrator 分发落盘任务 (SSOT)
	GetOrchestrator().Dispatch(CmdCommitBatch, task)

	// 5. 实时推送 (UI 即时响应)
	leaderboard := p.AnalyzeGas(block)
	p.pushEvents(block, activities, leaderboard)

	// 记录处理耗时 and 更新同步高度 (逻辑水位)
	p.updateMetrics(start, block)

	return nil
}

// handleReorgReadOnly 只读版本的 reorg 检测
func (p *Processor) handleReorgReadOnly(ctx context.Context, blockNum *big.Int, parentHash common.Hash) error {
	var lastBlock models.Block
	err := p.db.GetContext(ctx, &lastBlock, "SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = $1", new(big.Int).Sub(blockNum, big.NewInt(1)).String())
	if err == nil && lastBlock.Hash != parentHash.Hex() {
		return ReorgError{At: new(big.Int).Set(blockNum)}
	}
	return nil
}

// extractActivities 提取活动 (纯内存逻辑)
func (p *Processor) extractActivities(ctx context.Context, blockNum *big.Int, logs []types.Log, transactions types.Transactions) []models.Transfer {
	var activities []models.Transfer
	txWithRealLogs := make(map[string]bool)

	for _, vLog := range logs {
		activity := p.ProcessLog(vLog)
		if activity != nil {
			txWithRealLogs[activity.TxHash] = true
			activities = append(activities, *activity)
		}
	}

	syntheticIdx := uint(20000)
	for _, tx := range transactions {
		msg, err := types.Sender(types.LatestSignerForChainID(big.NewInt(p.chainID)), tx)
		fromAddr := "0xunknown"
		if err == nil {
			fromAddr = msg.Hex()
		}

		if faucet := p.detectFaucetNoDB(ctx, blockNum, tx, fromAddr, syntheticIdx); faucet != nil {
			activities = append(activities, *faucet)
			syntheticIdx++
			continue
		}

		if deploy := p.detectDeployNoDB(ctx, blockNum, tx, fromAddr, syntheticIdx); deploy != nil {
			activities = append(activities, *deploy)
			syntheticIdx++
			continue
		}

		if eth := p.detectEthTransferNoDB(ctx, blockNum, tx, fromAddr, syntheticIdx, txWithRealLogs); eth != nil {
			activities = append(activities, *eth)
			syntheticIdx++
		}
	}
	return activities
}

// detectFaucetNoDB 不写库的 faucet 检测
func (p *Processor) detectFaucetNoDB(ctx context.Context, blockNum *big.Int, tx *types.Transaction, fromAddr string, idx uint) *models.Transfer {
	faucetLabel := GetAddressLabel(fromAddr)
	if faucetLabel == "" {
		return nil
	}

	return &models.Transfer{
		BlockNumber: models.BigInt{Int: blockNum},
		TxHash:      tx.Hash().Hex(),
		LogIndex:    idx,
		From:        strings.ToLower(fromAddr),
		To: strings.ToLower(func() string {
			if tx.To() == nil {
				return "0xcontract_creation"
			}
			return tx.To().Hex()
		}()),
		Amount:       models.NewUint256FromBigInt(tx.Value()),
		TokenAddress: "0x0000000000000000000000000000000000000000",
		Symbol:       faucetLabel,
		Type:         "FAUCET_CLAIM",
	}
}

// detectDeployNoDB 不写库的合约部署检测
func (p *Processor) detectDeployNoDB(ctx context.Context, blockNum *big.Int, tx *types.Transaction, fromAddr string, idx uint) *models.Transfer {
	if tx.To() != nil {
		return nil
	}
	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: blockNum},
		TxHash:       tx.Hash().Hex(),
		LogIndex:     idx,
		From:         strings.ToLower(fromAddr),
		To:           "0xcontract_creation",
		Amount:       models.NewUint256FromBigInt(tx.Value()),
		TokenAddress: "0x0000000000000000000000000000000000000000",
		Symbol:       "EVM",
		Type:         "DEPLOY",
	}
}

// detectEthTransferNoDB 不写库的 ETH 转账检测
func (p *Processor) detectEthTransferNoDB(ctx context.Context, blockNum *big.Int, tx *types.Transaction, fromAddr string, idx uint, txWithRealLogs map[string]bool) *models.Transfer {
	if tx.Value().Cmp(big.NewInt(0)) <= 0 || txWithRealLogs[tx.Hash().Hex()] || tx.To() == nil {
		return nil
	}
	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: blockNum},
		TxHash:       tx.Hash().Hex(),
		LogIndex:     idx,
		From:         strings.ToLower(fromAddr),
		To:           strings.ToLower(tx.To().Hex()),
		Amount:       models.NewUint256FromBigInt(tx.Value()),
		TokenAddress: "0x0000000000000000000000000000000000000000",
		Symbol:       "ETH",
		Type:         "ETH_TRANSFER",
	}
}

// processAnvilSyntheticNoDB 不写库的 Anvil 模拟逻辑
func (p *Processor) processAnvilSyntheticNoDB(ctx context.Context, blockNum *big.Int, block *types.Block, activities []models.Transfer) []models.Transfer {
	if len(activities) > 0 || !p.enableSimulator || p.networkMode != "anvil" {
		return activities
	}

	numMocks := 2 + secureIntn(4)
	for i := 0; i < numMocks; i++ {
		mockFrom := p.getAnvilAccount(i)
		mockTo := p.getAnvilAccount(i + 1)
		mockAmount := big.NewInt(int64(100 + secureIntn(1000)))

		anvilTransfer := models.Transfer{
			BlockNumber:  models.BigInt{Int: blockNum},
			TxHash:       common.BytesToHash(append(block.Hash().Bytes(), []byte(fmt.Sprintf("ANVIL_MOCK_%d", i))...)).Hex(),
			LogIndex:     uint(99990 + i),
			From:         strings.ToLower(mockFrom),
			To:           strings.ToLower(mockTo),
			Amount:       models.NewUint256FromBigInt(mockAmount),
			TokenAddress: "0x0000000000000000000000000000000000000000",
			Symbol:       "ETH",
			Type:         "TRANSFER",
		}
		activities = append(activities, anvilTransfer)
	}
	return activities
}

func (p *Processor) getAnvilAccount(index int) string {
	accounts := []string{
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		"0x70997970C51812dc3A010C7d01b50e0d17dc79ee",
		"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		"0x90F79bf6EB2c4f870365E785982E1f101E93b906",
		"0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65",
	}
	return accounts[index%len(accounts)]
}

func (p *Processor) pushEvents(block *types.Block, activities []models.Transfer, leaderboard []models.GasSpender) {
	if p.EventHook == nil {
		return
	}
	latencyMs := time.Since(time.Unix(int64(block.Time()), 0)).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}

	latencyDisplay := fmt.Sprintf("%dms", latencyMs)
	if p.chainID == 31337 && latencyMs > 3600000 {
		latencyDisplay = "0.00s (Replay)"
	}

	latestChain := int64(0)
	syncLag := int64(0)
	if p.metrics != nil {
		latestChain = p.metrics.lastChainHeight.Load()
		syncLag = latestChain - int64(block.NumberU64())
		if syncLag < 0 {
			syncLag = 0
		}
	}

	p.EventHook("block", map[string]interface{}{
		"number":          block.NumberU64(),
		"hash":            block.Hash().Hex(),
		"parent_hash":     block.ParentHash().Hex(),
		"timestamp":       block.Time(),
		"tx_count":        len(block.Transactions()),
		"latency_ms":      latencyMs,
		"latency_display": latencyDisplay,
		"latest_chain":    latestChain,
		"sync_lag":        syncLag,
		"tps":             p.metrics.GetWindowTPS(),
	})
	p.EventHook("gas_leaderboard", leaderboard)
	if p.metrics != nil {
		p.metrics.RecordActivity(len(activities))
	}
	for _, t := range activities {
		if p.metrics != nil {
			p.metrics.TransfersProcessed.Inc()
			p.metrics.TransactionTypesTotal.WithLabelValues(t.Type).Inc()
		}

		p.EventHook("transfer", map[string]interface{}{"tx_hash": t.TxHash, "from": t.From, "to": t.To, "value": t.Amount.String(), "block_number": t.BlockNumber.String(), "token_address": t.TokenAddress, "symbol": t.Symbol, "type": t.Type, "log_index": t.LogIndex})
	}
}

func (p *Processor) updateMetrics(start time.Time, block *types.Block) {
	if p.metrics == nil {
		return
	}
	p.metrics.RecordBlockProcessed(time.Since(start))
	if block.Number().IsInt64() {
		p.metrics.UpdateCurrentSyncHeight(block.Number().Int64())
		latency := time.Since(time.Unix(int64(block.Time()), 0)).Seconds()
		if latency < 0 {
			latency = 0
		}
		p.metrics.UpdateE2ELatency(latency)
	}
}

// AnalyzeGas 实时分析区块中的 Gas 消耗大户
func (p *Processor) AnalyzeGas(block *types.Block) []models.GasSpender {
	spenders := make(map[string]*models.GasSpender)

	for _, tx := range block.Transactions() {
		to := "0xcontract_creation"
		if tx.To() != nil {
			to = strings.ToLower(tx.To().Hex())
		}

		fee := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasPrice())

		if s, exists := spenders[to]; exists {
			s.TotalGas += tx.Gas()
			existingFee, _ := new(big.Int).SetString(s.TotalFee, 10)
			if existingFee == nil {
				existingFee = big.NewInt(0)
			}
			s.TotalFee = new(big.Int).Add(existingFee, fee).String()
		} else {
			label := GetAddressLabel(to)
			spenders[to] = &models.GasSpender{
				Address:  to,
				Label:    label,
				TotalGas: tx.Gas(),
				TotalFee: fee.String(),
			}
		}
	}

	result := make([]models.GasSpender, 0, len(spenders))
	for _, s := range spenders {
		f, _ := new(big.Int).SetString(s.TotalFee, 10)
		ethVal := new(big.Float).SetInt(f)
		ethVal.Quo(ethVal, new(big.Float).SetFloat64(1e18))
		s.TotalFee = fmt.Sprintf("%.4f", ethVal)
		result = append(result, *s)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalGas > result[j].TotalGas
	})

	if len(result) > 5 {
		result = result[:5]
	}
	return result
}
