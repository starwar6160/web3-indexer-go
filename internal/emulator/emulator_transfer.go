package emulator

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func (e *Emulator) sendTransfer(ctx context.Context) {
	// 每次发送前检查并补充余额 (6个9持久性保障)
	if err := e.ensureBalance(ctx); err != nil {
		e.logger.Warn("balance_check_failed", slog.String("error", err.Error()))
	}

	nonce, err := e.nm.GetNextNonce(ctx)
	if err != nil {
		return
	}

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		return
	}

	// 限制最大 Gas Price
	maxPrice := new(big.Int).Mul(big.NewInt(e.maxGasPrice), big.NewInt(1e9))
	if gasPrice.Cmp(maxPrice) > 0 {
		gasPrice = maxPrice
	}

	randomVal, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		e.logger.Error("random_val_generation_failed", slog.Any("err", err))
		return
	}
	transferVal := new(big.Int).Add(randomVal, big.NewInt(1))

	methodID := common.FromHex("0xa9059cbb")
	// 演示级：随机生成接收地址，增加视觉丰富度
	randomAddrBytes := make([]byte, 20)
	if _, err := rand.Read(randomAddrBytes); err != nil {
		e.logger.Error("random_addr_generation_failed", slog.Any("err", err))
		return
	}
	targetAddr := common.BytesToAddress(randomAddrBytes)

	toAddr := common.LeftPadBytes(targetAddr.Bytes(), 32)
	amount := common.LeftPadBytes(transferVal.Bytes(), 32)

	var data []byte
	data = append(data, methodID...)
	data = append(data, toAddr...)
	data = append(data, amount...)

	estimatedGas, err := e.client.EstimateGas(ctx, ethereum.CallMsg{From: e.fromAddr, To: &e.contract, Data: data})
	if err != nil {
		estimatedGas = 100000
	} else {
		// 应用动态安全裕度
		// #nosec G115
		estimatedGas += (estimatedGas * uint64(e.gasSafetyMargin) / 100)
	}

	tx := types.NewTransaction(nonce, e.contract, big.NewInt(0), estimatedGas, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		e.Metrics.Failed.Add(1)
		return
	}

	if err := e.client.SendTransaction(ctx, signedTx); err != nil {
		e.Metrics.Failed.Add(1)
		e.logger.Error("send_failed", slog.String("error", err.Error()), slog.Uint64("nonce", nonce))
		// ---------------- 自修复逻辑 ----------------
		if strings.Contains(err.Error(), "nonce too low") || strings.Contains(err.Error(), "already known") {
			e.logger.Warn("🚨 NONCE_OUT_OF_SYNC", slog.Uint64("failed_nonce", nonce))
			e.Metrics.SelfHealed.Add(1)
			if e.OnSelfHealing != nil {
				e.OnSelfHealing("nonce_mismatch")
			}
			_ = e.nm.ResyncNonce(ctx)
		} else {
			// 对于其他网络错误，尝试回滚 nonce 以便下次重试该号
			e.nm.RollbackNonce(nonce)
		}
		// -------------------------------------------
		return
	}

	e.Metrics.Sent.Add(1)
	if e.OnMetrics != nil {
		// Get individual values to avoid copying atomic values
		sent := e.Metrics.Sent.Load()
		confirmed := e.Metrics.Confirmed.Load()
		failed := e.Metrics.Failed.Load()
		selfHealed := e.Metrics.SelfHealed.Load()

		// Create a snapshot of metrics
		metricsSnapshot := MetricsSnapshot{
			Sent:       sent,
			Confirmed:  confirmed,
			Failed:     failed,
			SelfHealed: selfHealed,
		}

		e.OnMetrics(metricsSnapshot)
	}

	e.logger.Info("📤 [Emulator] Sent REAL Transfer",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.String("to", targetAddr.Hex()),
		slog.String("amount", transferVal.String()),
		slog.Uint64("nonce", nonce),
	)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("emulator_receipt_wait_panic", "err", r)
			}
		}()
		receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
		if err == nil {
			e.Metrics.Confirmed.Add(1)
			e.logger.Info("✅ [Emulator] Confirmed", slog.String("hash", signedTx.Hash().Hex()[:10]), slog.Uint64("block", receipt.BlockNumber.Uint64()))
		}
	}()
}

func (e *Emulator) waitForReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			receipt, err := e.client.TransactionReceipt(ctx, hash)
			if err == nil {
				return receipt, nil
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout")
			}
		}
	}
}
