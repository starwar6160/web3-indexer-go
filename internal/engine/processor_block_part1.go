package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"time"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
)

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
	dbTx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		LogTransactionFailed("begin_transaction", blockNum.String(), err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// 无论成功失败，确保 Rollback (Commit 后 Rollback 无效)
	defer func() {
		if err := dbTx.Rollback(); err != nil && err != sql.ErrTxDone {
			Logger.Warn("block_rollback_failed", "err", err)
		}
	}()

	// 1. Reorg 检测 (Parent Hash Check)
	if err := p.handleReorg(ctx, dbTx, blockNum, block.ParentHash()); err != nil {
		return err
	}

	// 2. 写入 Block
	if err := p.insertBlock(ctx, dbTx, block); err != nil {
		return err
	}

	// 3. 处理链上活动
	activities, _ := p.processActivities(ctx, dbTx, blockNum, data.Logs, block.Transactions())

	// 🚀 模拟模式：强制生成 Synthetic Transfer（让空链也有数据）
	activities = p.processAnvilSynthetic(ctx, dbTx, blockNum, block, activities)

	// 4. 更新 Checkpoint（按批次更新以提升性能）
	p.handleCheckpoint(ctx, dbTx, blockNum, data.RangeEnd)

	// 5. 提交事务
	if err := dbTx.Commit(); err != nil {
		LogTransactionFailed("commit_transaction", blockNum.String(), err)
		return fmt.Errorf("failed to commit transaction for block %s: %w", blockNum.String(), err)
	}

	// 🚀 核心增强：执行 Gas 大户分析
	leaderboard := p.AnalyzeGas(block)

	// 6. 实时事件推送 (在事务成功后)
	p.pushEvents(block, activities, leaderboard)

	// 记录处理耗时 and 当前同步高度
	p.updateMetrics(start, block)

	return nil
}

func (p *Processor) processActivities(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, logs []types.Log, transactions types.Transactions) ([]models.Transfer, map[string]bool) {
	var activities []models.Transfer
	txWithRealLogs := make(map[string]bool)

	for _, vLog := range logs {
		activity := p.ProcessLog(vLog)
		if activity != nil {
			_, err := dbTx.NamedExecContext(ctx, `
				INSERT INTO transfers
				(block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
				VALUES
				(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
				ON CONFLICT (block_number, log_index) DO NOTHING
			`, activity)
			if err == nil {
				txWithRealLogs[activity.TxHash] = true
				activities = append(activities, *activity)
			}
		}
	}

	syntheticIdx := uint(20000)
	for _, tx := range transactions {
		msg, err := types.Sender(types.LatestSignerForChainID(big.NewInt(p.chainID)), tx)
		fromAddr := "0xunknown"
		if err == nil {
			fromAddr = msg.Hex()
		}

		if faucet := p.detectFaucet(ctx, dbTx, blockNum, tx, fromAddr, syntheticIdx); faucet != nil {
			activities = append(activities, *faucet)
			syntheticIdx++
			continue
		}

		if deploy := p.detectDeploy(ctx, dbTx, blockNum, tx, fromAddr, syntheticIdx); deploy != nil {
			activities = append(activities, *deploy)
			syntheticIdx++
			continue
		}

		if eth := p.detectEthTransfer(ctx, dbTx, blockNum, tx, fromAddr, syntheticIdx, txWithRealLogs); eth != nil {
			activities = append(activities, *eth)
			syntheticIdx++
		}
	}
	return activities, txWithRealLogs
}

func (p *Processor) detectFaucet(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, tx *types.Transaction, fromAddr string, idx uint) *models.Transfer {
	faucetLabel := GetAddressLabel(fromAddr)
	if faucetLabel == "" {
		return nil
	}

	activity := &models.Transfer{
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
	if _, err := dbTx.NamedExecContext(ctx, `INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type) VALUES (:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type) ON CONFLICT DO NOTHING`, activity); err != nil {
		Logger.Warn("failed_to_insert_detected_activity", "err", err)
	}
	return activity
}

func (p *Processor) detectDeploy(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, tx *types.Transaction, fromAddr string, idx uint) *models.Transfer {
	if tx.To() != nil {
		return nil
	}
	activity := &models.Transfer{
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
	if _, err := dbTx.NamedExecContext(ctx, `INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type) VALUES (:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type) ON CONFLICT DO NOTHING`, activity); err != nil {
		Logger.Warn("failed_to_insert_detected_activity", "err", err)
	}
	return activity
}

func (p *Processor) detectEthTransfer(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, tx *types.Transaction, fromAddr string, idx uint, txWithRealLogs map[string]bool) *models.Transfer {
	if tx.Value().Cmp(big.NewInt(0)) <= 0 || txWithRealLogs[tx.Hash().Hex()] || tx.To() == nil {
		return nil
	}
	activity := &models.Transfer{
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
	if _, err := dbTx.NamedExecContext(ctx, `INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type) VALUES (:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type) ON CONFLICT DO NOTHING`, activity); err != nil {
		Logger.Warn("failed_to_insert_eth_transfer", "err", err)
	}
	return activity
}

func (p *Processor) processAnvilSynthetic(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, block *types.Block, activities []models.Transfer) []models.Transfer {
	if len(activities) > 0 || !p.enableSimulator || p.networkMode != "anvil" {
		return activities
	}

	// 🚀 工业级增强：一次生成多笔交易，提升 TPS 视觉效果
	numMocks := 2 + secureIntn(4) // 2-5 笔
	for i := 0; i < numMocks; i++ {
		mockFrom := p.getAnvilAccount(i)
		mockTo := p.getAnvilAccount(i + 1)
		mockAmount := big.NewInt(int64(100 + secureIntn(1000)))

		anvilTransfer := models.Transfer{
			BlockNumber: models.BigInt{Int: blockNum},
			TxHash:      common.BytesToHash(append(block.Hash().Bytes(), []byte(fmt.Sprintf("ANVIL_MOCK_%d", i))...)).Hex(),
			// #nosec G115
			LogIndex:     uint(99990 + i),
			From:         strings.ToLower(mockFrom),
			To:           strings.ToLower(mockTo),
			Amount:       models.NewUint256FromBigInt(mockAmount),
			TokenAddress: "0x0000000000000000000000000000000000000000",
			Symbol:       "ETH", // 给它一个符号
			Type:         "TRANSFER",
		}

		if _, err := dbTx.NamedExecContext(ctx, `INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type) VALUES (:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type) ON CONFLICT DO NOTHING`, anvilTransfer); err != nil {
			Logger.Error("failed_to_insert_anvil_synthetic_transfer", "err", err)
		} else {
			activities = append(activities, anvilTransfer)
		}
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

func (p *Processor) handleCheckpoint(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, rangeEnd *big.Int) {
	p.blocksSinceLastCheckpoint++
	checkpointTarget := blockNum
	shouldUpdateCheckpoint := p.blocksSinceLastCheckpoint >= p.checkpointBatch
	if rangeEnd != nil && rangeEnd.Cmp(blockNum) >= 0 {
		checkpointTarget = rangeEnd
		shouldUpdateCheckpoint = true
	}
	if shouldUpdateCheckpoint {
		if err := p.updateCheckpointInTx(ctx, dbTx, p.chainID, checkpointTarget); err != nil {
			Logger.Warn("failed_to_update_checkpoint", "err", err)
		}
		p.blocksSinceLastCheckpoint = 0
	}
}

func (p *Processor) pushEvents(block *types.Block, activities []models.Transfer, leaderboard []models.GasSpender) {
	if p.EventHook == nil {
		return
	}
	// #nosec G115
	latency := time.Since(time.Unix(int64(block.Time()), 0)).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	p.EventHook("block", map[string]interface{}{
		"number":      block.NumberU64(),
		"hash":        block.Hash().Hex(),
		"parent_hash": block.ParentHash().Hex(),
		"timestamp":   block.Time(),
		"tx_count":    len(block.Transactions()),
		"latency_ms":  latency,
	})
	p.EventHook("gas_leaderboard", leaderboard)
	for _, t := range activities {
		// 🚀 工业级对齐：同步更新 Prometheus 计数器，确保与 UI 彻底同步
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
		// #nosec G115
		latency := time.Since(time.Unix(int64(block.Time()), 0)).Seconds()
		if latency < 0 {
			latency = 0
		}
		p.metrics.UpdateE2ELatency(latency)
	}
}

func (p *Processor) handleReorg(ctx context.Context, dbTx *sqlx.Tx, blockNum *big.Int, parentHash common.Hash) error {
	var lastBlock models.Block
	err := dbTx.GetContext(ctx, &lastBlock, "SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = $1", new(big.Int).Sub(blockNum, big.NewInt(1)).String())
	if err == nil && lastBlock.Hash != parentHash.Hex() {
		return ReorgError{At: new(big.Int).Set(blockNum)}
	}
	return nil
}

func (p *Processor) insertBlock(ctx context.Context, dbTx *sqlx.Tx, block *types.Block) error {
	var baseFee *models.BigInt
	if block.BaseFee() != nil {
		baseFee = &models.BigInt{Int: block.BaseFee()}
	}
	_, err := dbTx.NamedExecContext(ctx, `INSERT INTO blocks (number, hash, parent_hash, timestamp, gas_limit, gas_used, base_fee_per_gas, transaction_count) VALUES (:number, :hash, :parent_hash, :timestamp, :gas_limit, :gas_used, :base_fee_per_gas, :transaction_count) ON CONFLICT (number) DO UPDATE SET hash = EXCLUDED.hash, parent_hash = EXCLUDED.parent_hash, timestamp = EXCLUDED.timestamp, gas_limit = EXCLUDED.gas_limit, gas_used = EXCLUDED.gas_used, base_fee_per_gas = EXCLUDED.base_fee_per_gas, transaction_count = EXCLUDED.transaction_count, processed_at = NOW()`, models.Block{
		Number:           models.BigInt{Int: block.Number()},
		Hash:             block.Hash().Hex(),
		ParentHash:       block.ParentHash().Hex(),
		Timestamp:        block.Time(),
		GasLimit:         block.GasLimit(),
		GasUsed:          block.GasUsed(),
		BaseFeePerGas:    baseFee,
		TransactionCount: len(block.Transactions()),
	})
	return err
}

// AnalyzeGas 实时分析区块中的 Gas 消耗大户
func (p *Processor) AnalyzeGas(block *types.Block) []models.GasSpender {
	spenders := make(map[string]*models.GasSpender)

	for _, tx := range block.Transactions() {
		to := "0xcontract_creation"
		if tx.To() != nil {
			to = strings.ToLower(tx.To().Hex())
		}

		// 计算费用 (GasUsed * GasPrice)
		// 注意：此处 tx.Gas() 是 Limit，实际应使用 Receipt 中的 GasUsed，但为了实时性，此处用 Limit 估算
		fee := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasPrice())

		if s, exists := spenders[to]; exists {
			s.TotalGas += tx.Gas()
			// 将 fee 加到总费用中
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

	// 转换为 Slice 并排序
	result := make([]models.GasSpender, 0, len(spenders))
	for _, s := range spenders {
		// 格式化费用为 ETH (粗略计算)
		f, _ := new(big.Int).SetString(s.TotalFee, 10)
		ethVal := new(big.Float).SetInt(f)
		ethVal.Quo(ethVal, new(big.Float).SetFloat64(1e18))
		s.TotalFee = fmt.Sprintf("%.4f", ethVal)
		result = append(result, *s)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalGas > result[j].TotalGas
	})

	// 取 Top 5
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}
