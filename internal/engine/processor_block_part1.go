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
	defer dbTx.Rollback()

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

	// 🛡️ 工业级逻辑守卫：零值父哈希防护
	parentHashHex := block.ParentHash().Hex()
	if parentHashHex == "" || parentHashHex == "0x0000000000000000000000000000000000000000000000000000000000000000" {
		Logger.Error("❌ FATAL: Zero parent hash detected!", 
			slog.String("block", blockNum.String()))
		return fmt.Errorf("zero parent hash detected at block %s", blockNum.String())
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

	// 3. 处理 Transfer 事件（如果日志中有）
	var transfers []models.Transfer         // 用于实时推送
	txWithRealLogs := make(map[string]bool) // track tx hashes that produced real Transfer logs
	if len(data.Logs) > 0 {
		Logger.Debug("scanning_logs",
			slog.String("block", blockNum.String()),
			slog.Int("logs_count", len(data.Logs)),
		)
	}

	for i, vLog := range data.Logs {
		Logger.Debug("🔍 正在扫描区块中的 Log...",
			slog.String("stage", "PROCESSOR"),
			slog.Int("index", i),
			slog.String("log_address", vLog.Address.Hex()),
			slog.String("topic0", vLog.Topics[0].Hex()),
		)

		// 检查地址匹配逻辑
		logAddrLow := strings.ToLower(vLog.Address.Hex())
		isMatched := false
		for addr := range p.watchedAddresses {
			if strings.ToLower(addr.Hex()) == logAddrLow {
				isMatched = true
				break
			}
		}

		if isMatched || len(p.watchedAddresses) == 0 {
			if len(p.watchedAddresses) > 0 {
				Logger.Info("🎯 发现匹配合约地址！",
					slog.String("stage", "PROCESSOR"),
					slog.String("address", logAddrLow),
				)
			}

			// 检查 Topic 匹配
			if vLog.Topics[0] == TransferEventHash {
				Logger.Info("✨ 发现 Transfer 事件 Topic！",
					slog.String("stage", "PROCESSOR"),
					slog.String("tx_hash", vLog.TxHash.Hex()),
				)

				transfer := p.ExtractTransfer(vLog)
				if transfer != nil {
					Logger.Info("📦 解析成功，准备入库",
						slog.String("stage", "PROCESSOR"),
						slog.String("from", transfer.From),
						slog.String("to", transfer.To),
						slog.String("amount", transfer.Amount.String()),
					)

					_, err = dbTx.NamedExecContext(ctx, `
						INSERT INTO transfers
						(block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
						VALUES
						(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address)
						ON CONFLICT (block_number, log_index) DO NOTHING
					`, transfer)
					if err != nil {
						Logger.Error("❌ 数据库写入失败",
							slog.String("stage", "PROCESSOR"),
							slog.String("error", err.Error()),
							slog.String("tx_hash", transfer.TxHash),
						)
						if p.metrics != nil {
							p.metrics.RecordTransferFailed()
						}
						return fmt.Errorf("failed to insert transfer at block %s: %w", blockNum.String(), err)
					}
					txWithRealLogs[transfer.TxHash] = true
					transfers = append(transfers, *transfer)
					if p.metrics != nil {
						p.metrics.RecordTransferProcessed()
					}
					Logger.Info("✅ Transfer saved to DB",
						slog.String("stage", "PROCESSOR"),
						slog.String("block", blockNum.String()),
						slog.String("tx_hash", transfer.TxHash),
					)
				} else {
					Logger.Warn("❌ Transfer 解析失败",
						slog.String("stage", "PROCESSOR"),
						slog.String("tx_hash", vLog.TxHash.Hex()),
					)
				}
			}
		}
	}

	// Fallback: Scan transactions for direct calls to watched addresses (in case logs are missing/filtered)
	Logger.Debug("fallback_scanning_transactions",
		slog.String("block", blockNum.String()),
		slog.Int("tx_count", len(data.Block.Transactions())),
	)
	syntheticIdx := uint(10000) // high base to avoid conflict with real log_index
	for _, tx := range data.Block.Transactions() {
		toAddr := "[Contract Creation]"
		if tx.To() != nil {
			txToLow := strings.ToLower(tx.To().Hex())
			isMatched := false
			for addr := range p.watchedAddresses {
				if strings.ToLower(addr.Hex()) == txToLow {
					isMatched = true
					break
				}
			}

			// In DemoMode or if no addresses configured, match all for debug
			if len(p.watchedAddresses) == 0 {
				isMatched = true
			}

			if isMatched && !txWithRealLogs[tx.Hash().Hex()] {
				toAddr = txToLow
				Logger.Info("🎯 发现匹配交易",
					slog.String("stage", "PROCESSOR"),
					slog.String("tx_hash", tx.Hash().Hex()),
					slog.String("to", txToLow),
				)

				// 构造一个合成的 Transfer 事件 (尝试从交易中提取真实地址)
				input := tx.Data()
				syntheticAmount := big.NewInt(1000) // 默认值
				if len(input) >= 68 {
					// 提取第 4-36 字节作为 To 地址 (ERC20 transfer 参数)
					toAddr = common.BytesToAddress(input[16:36]).Hex()
					// 提取最后 32 字节作为金额
					syntheticAmount = new(big.Int).SetBytes(input[len(input)-32:])
				}

				// 尝试获取发送者 (使用正确的 EIP155 Signer)
				fromAddr := "[Contract_Call]"
				signer := types.LatestSignerForChainID(big.NewInt(p.chainID))
				if sender, err := types.Sender(signer, tx); err == nil {
					fromAddr = sender.Hex()
				}

				syntheticTransfer := &models.Transfer{
					BlockNumber:  models.BigInt{Int: blockNum},
					TxHash:       tx.Hash().Hex(),
					LogIndex:     syntheticIdx,
					From:         strings.ToLower(fromAddr),
					To:           strings.ToLower(toAddr),
					Amount:       models.NewUint256FromBigInt(syntheticAmount),
					TokenAddress: txToLow,
				}
				syntheticIdx++

				_, err = dbTx.NamedExecContext(ctx, `
					INSERT INTO transfers
					(block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
					VALUES
					(:block_number, :tx_hash, :log_index, :from_address, :to_address, :amount, :token_address)
					ON CONFLICT (block_number, log_index) DO NOTHING
				`, syntheticTransfer)
				if err == nil {
					transfers = append(transfers, *syntheticTransfer)
					Logger.Info("✅ Synthetic Transfer saved to DB",
						slog.String("stage", "PROCESSOR"),
						slog.String("tx_hash", tx.Hash().Hex()),
					)
				}
			}
		}
	}

	// 4. 更新 Checkpoint（按批次更新以提升性能）
	p.blocksSinceLastCheckpoint++
	if p.blocksSinceLastCheckpoint >= p.checkpointBatch {
		if err := p.updateCheckpointInTx(ctx, dbTx, 1, blockNum); err != nil {
			return fmt.Errorf("failed to update checkpoint for block %s: %w", blockNum.String(), err)
		}
		p.blocksSinceLastCheckpoint = 0
		Logger.Debug("checkpoint_persisted_batch", slog.String("block", blockNum.String()))
	}

	// 5. 提交事务
	if err := dbTx.Commit(); err != nil {
		LogTransactionFailed("commit_transaction", blockNum.String(), err)
		return fmt.Errorf("failed to commit transaction for block %s: %w", blockNum.String(), err)
	}

	// 6. 实时事件推送 (在事务成功后)
	if p.EventHook != nil {
		// 计算端到端延迟 (毫秒)
		latency := time.Since(time.Unix(int64(block.Time()), 0)).Milliseconds()
		if latency < 0 { latency = 0 }

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

		for _, t := range transfers {
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

	// 记录处理耗时 and 当前同步高度
	if p.metrics != nil {
		p.metrics.RecordBlockProcessed(time.Since(start))
		// 更新当前同步高度 gauge (增加溢出安全性检查)
		if blockNum.IsInt64() {
			p.metrics.UpdateCurrentSyncHeight(blockNum.Int64())
		} else {
			Logger.Warn("block_number_overflows_int64_for_metrics", slog.String("block", blockNum.String()))
		}
	}

	return nil
}