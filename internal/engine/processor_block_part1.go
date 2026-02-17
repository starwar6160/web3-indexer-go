package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
	var lastBlock models.Block
	err = dbTx.GetContext(ctx, &lastBlock,
		"SELECT number, hash, parent_hash, timestamp FROM blocks WHERE number = $1",
		new(big.Int).Sub(blockNum, big.NewInt(1)).String())

	if err == nil {
		// 如果找到了上一个区块，检查 Hash 链
		if lastBlock.Hash != block.ParentHash().Hex() {
			LogReorgDetected(blockNum.String(), lastBlock.Hash, block.ParentHash().Hex())
			if p.EventHook != nil {
				p.EventHook("log", map[string]interface{}{
					"message": fmt.Sprintf("🚨 REORG DETECTED at #%s! Rolling back...", blockNum.String()),
					"level":   "error",
				})
			}
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
	var baseFee *models.BigInt
	if block.BaseFee() != nil {
		baseFee = &models.BigInt{Int: block.BaseFee()}
	}

	// 🛡️ 工业级逻辑守卫：哈希自指检测
	if block.Hash().Hex() == block.ParentHash().Hex() {
		Logger.Error("❌ FATAL: Block hash equals parent hash!",
			slog.String("block", blockNum.String()),
			slog.String("hash", block.Hash().Hex()))
		return fmt.Errorf("hash self-reference detected at block %s", blockNum.String())
	}

	// 🛡️ 工业级逻辑守卫：零值父哈希防护 (针对非 Genesis 块)
	parentHashHex := block.ParentHash().Hex()
	if blockNum.Cmp(big.NewInt(0)) > 0 && (parentHashHex == "" || parentHashHex == "0x0000000000000000000000000000000000000000000000000000000000000000") {
		Logger.Warn("⚠️ Zero parent hash detected for non-genesis block",
			slog.String("block", blockNum.String()))
		// 允许继续，但在日志中记录，这通常发生在链的极早期或者测试网模拟中
	}

	_, err = dbTx.NamedExecContext(ctx, `
		INSERT INTO blocks (number, hash, parent_hash, timestamp, gas_limit, gas_used, base_fee_per_gas, transaction_count)
		VALUES (:number, :hash, :parent_hash, :timestamp, :gas_limit, :gas_used, :base_fee_per_gas, :transaction_count)
		ON CONFLICT (number) DO UPDATE SET
			hash = EXCLUDED.hash,
			parent_hash = EXCLUDED.parent_hash,
			timestamp = EXCLUDED.timestamp,
			gas_limit = EXCLUDED.gas_limit,
			gas_used = EXCLUDED.gas_used,
			base_fee_per_gas = EXCLUDED.base_fee_per_gas,
			transaction_count = EXCLUDED.transaction_count,
			processed_at = NOW()
	`, models.Block{
		Number:           models.BigInt{Int: blockNum},
		Hash:             block.Hash().Hex(),
		ParentHash:       block.ParentHash().Hex(),
		Timestamp:        block.Time(),
		GasLimit:         block.GasLimit(),
		GasUsed:          block.GasUsed(),
		BaseFeePerGas:    baseFee,
		TransactionCount: len(block.Transactions()),
	})
	if err != nil {
		LogTransactionFailed("insert_block", blockNum.String(), err)
		return fmt.Errorf("failed to insert block: %w", err)
	}

	// 3. 处理链上活动
	var activities []models.Transfer        // 用于实时推送
	txWithRealLogs := make(map[string]bool) // track tx hashes that produced logs
	
	// A. 扫描所有日志 (全量嗅探模式)
	for _, vLog := range data.Logs {
		activity := p.ProcessLog(vLog)
		if activity != nil {
			_, err = dbTx.NamedExecContext(ctx, `
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

	// B. 扫描交易列表 (捕获部署与原生转账)
	syntheticIdx := uint(20000) // 业务逻辑偏移量，避免与 LogIndex 冲突
	for _, tx := range block.Transactions() {
		msg, err := types.Sender(types.LatestSignerForChainID(big.NewInt(p.chainID)), tx)
		fromAddr := "0xunknown"
		if err == nil {
			fromAddr = msg.Hex()
		}

		// 1. 识别合约部署
		if tx.To() == nil {
			deployActivity := models.Transfer{
				BlockNumber:  models.BigInt{Int: blockNum},
				TxHash:       tx.Hash().Hex(),
				LogIndex:     syntheticIdx,
				From:         strings.ToLower(fromAddr),
				To:           "0xcontract_creation",
				Amount:       models.NewUint256FromBigInt(tx.Value()),
				TokenAddress: "0x0000000000000000000000000000000000000000",
				Symbol:       "EVM",
				Type:         "DEPLOY",
			}
			_, _ = dbTx.NamedExecContext(ctx, `
				INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
				VALUES (:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
				ON CONFLICT DO NOTHING
			`, deployActivity)
			activities = append(activities, deployActivity)
			syntheticIdx++
			continue
		}

		// 2. 识别显著的原生 ETH 转账 (比如非零转账且未被 Log 捕获)
		if tx.Value().Cmp(big.NewInt(0)) > 0 && !txWithRealLogs[tx.Hash().Hex()] {
			ethActivity := models.Transfer{
				BlockNumber:  models.BigInt{Int: blockNum},
				TxHash:       tx.Hash().Hex(),
				LogIndex:     syntheticIdx,
				From:         strings.ToLower(fromAddr),
				To:           strings.ToLower(tx.To().Hex()),
				Amount:       models.NewUint256FromBigInt(tx.Value()),
				TokenAddress: "0x0000000000000000000000000000000000000000",
				Symbol:       "ETH",
				Type:         "ETH_TRANSFER",
			}
			_, _ = dbTx.NamedExecContext(ctx, `
				INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
				VALUES (:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
				ON CONFLICT DO NOTHING
			`, ethActivity)
			activities = append(activities, ethActivity)
			syntheticIdx++
		}
	}

	// 🚀 模拟模式：强制生成 Synthetic Transfer（让空链也有数据）
	// 诊断：如果这个区块没有任何 Transfer（real + synthetic），则伪造一个
	if p.enableSimulator && p.networkMode == "anvil" {
		Logger.Info("🔍 [ANVIL] Checking if synthetic transfer needed",
			slog.String("block", blockNum.String()),
			slog.Int("existing_transfers", len(activities)),
		)
	}

	if len(activities) == 0 && p.enableSimulator && p.networkMode == "anvil" {
		// 生成一个模拟的 ETH 转账
		mockFrom := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" // Anvil Account #0
		mockTo := "0x70997970C51812dc3A010C7d01b50e0d17dc79ee"   // Anvil Account #1
		mockAmount := big.NewInt(int64(blockNum.Int64() % 1000000000)) // 伪随机金额

		anvilTransfer := &models.Transfer{
			BlockNumber:  models.BigInt{Int: blockNum},
			TxHash:       common.BytesToHash(append(block.Hash().Bytes(), []byte("ANVIL_MOCK")...)).Hex(),
			LogIndex:     99999, // 特殊标记
			From:         strings.ToLower(mockFrom),
			To:           strings.ToLower(mockTo),
			Amount:       models.NewUint256FromBigInt(mockAmount),
			TokenAddress: "0x0000000000000000000000000000000000000000", // ETH
			Type:         "TRANSFER",
		}

		_, err = dbTx.NamedExecContext(ctx, `
			INSERT INTO transfers
			(block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type)
			VALUES
			(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address, :symbol, :activity_type)
			ON CONFLICT (block_number, log_index) DO NOTHING
		`, anvilTransfer)

		if err == nil {
			activities = append(activities, *anvilTransfer)
			Logger.Info("🏭 [ANVIL] Synthetic Transfer generated",
				slog.String("stage", "PROCESSOR"),
				slog.String("block", blockNum.String()),
				slog.String("from", mockFrom),
				slog.String("to", mockTo),
				slog.String("amount", mockAmount.String()),
			)
		} else {
			Logger.Error("❌ [ANVIL] Failed to insert synthetic transfer",
				slog.String("block", blockNum.String()),
				slog.String("error", err.Error()),
			)
		}
	}

	// 4. 更新 Checkpoint（按批次更新以提升性能）
	p.blocksSinceLastCheckpoint++

	// 如果是范围抓取的最后一个块，或者达到了批次上限
	checkpointTarget := blockNum
	shouldUpdateCheckpoint := p.blocksSinceLastCheckpoint >= p.checkpointBatch

	if data.RangeEnd != nil && data.RangeEnd.Cmp(blockNum) >= 0 {
		checkpointTarget = data.RangeEnd
		shouldUpdateCheckpoint = true
	}

	if shouldUpdateCheckpoint {
		if err := p.updateCheckpointInTx(ctx, dbTx, p.chainID, checkpointTarget); err != nil {
			return fmt.Errorf("failed to update checkpoint for block %s: %w", checkpointTarget.String(), err)
		}
		p.blocksSinceLastCheckpoint = 0
		Logger.Debug("checkpoint_persisted", slog.String("block", checkpointTarget.String()))
	}

	// 5. 提交事务
	if err := dbTx.Commit(); err != nil {
		LogTransactionFailed("commit_transaction", blockNum.String(), err)
		return fmt.Errorf("failed to commit transaction for block %s: %w", blockNum.String(), err)
	}

	// 6. 实时事件推送 (在事务成功后)
	if p.EventHook != nil {
		// 计算端到端延迟 (毫秒)
		// #nosec G115 - Block time fits in int64
		latency := time.Since(time.Unix(int64(block.Time()), 0)).Milliseconds()
		if latency < 0 {
			latency = 0
		}

		p.EventHook("block", map[string]interface{}{
			"number":      block.NumberU64(),
			"hash":        block.Hash().Hex(),
			"parent_hash": block.ParentHash().Hex(), // 🚀 补齐这个关键字段
			"timestamp":   block.Time(),
			"tx_count":    len(block.Transactions()),
			"latency_ms":  latency,
		})

		p.EventHook("log", map[string]interface{}{
			"message": fmt.Sprintf("✅ Processed Block #%d (%d txs)", block.NumberU64(), len(block.Transactions())),
			"level":   "info",
		})

		for _, t := range activities {
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

	// 记录处理耗时 and 当前同步高度
	if p.metrics != nil {
		p.metrics.RecordBlockProcessed(time.Since(start))
		// 更新当前同步高度 gauge (增加溢出安全性检查)
		if blockNum.IsInt64() {
			p.metrics.UpdateCurrentSyncHeight(blockNum.Int64())
			slog.Debug("metrics_updated", "height", blockNum.Int64())

			// 计算并更新高精度 E2E Latency
			// #nosec G115
			blockTime := time.Unix(int64(block.Time()), 0)
			latency := time.Since(blockTime).Seconds()
			if latency < 0 {
				latency = 0
			}
			p.metrics.UpdateE2ELatency(latency)
		} else {
			Logger.Warn("block_number_overflows_int64_for_metrics", slog.String("block", blockNum.String()))
		}
	}

	return nil
}
