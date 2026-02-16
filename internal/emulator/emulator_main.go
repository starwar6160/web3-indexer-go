package emulator

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func (e *Emulator) Start(ctx context.Context, addressChan chan<- common.Address) error {
	e.logger.Info("emulator_starting",
		slog.String("from_address", e.fromAddr.Hex()),
		slog.String("chain_id", e.chainID.String()),
	)

	// 初始充值
	if err := e.ensureBalance(ctx); err != nil {
		e.logger.Warn("initial_funding_failed_proceeding", slog.String("error", err.Error()))
	}

	// 1. 自动部署合约
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	contractAddr, err := e.deployContract(deployCtx)
	cancel()
	if err != nil {
		return err
	}
	e.contract = contractAddr
	e.logger.Info("contract_deployed", slog.String("address", contractAddr.Hex()))

	if addressChan != nil {
		select {
		case addressChan <- contractAddr:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	txTicker := time.NewTicker(e.txInterval)
	defer txTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-txTicker.C:
			e.sendTransfer(ctx)
		}
	}
}

func (e *Emulator) deployContract(ctx context.Context) (common.Address, error) {
	nonce, err := e.nm.GetNextNonce(ctx)
	if err != nil {
		return common.Address{}, err
	}

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Address{}, err
	}

	// 限制最大 Gas Price
	maxPrice := new(big.Int).Mul(big.NewInt(e.maxGasPrice), big.NewInt(1e9))
	if gasPrice.Cmp(maxPrice) > 0 {
		e.logger.Warn("⚠️ Gas price exceeded limit, capping", slog.String("original", gasPrice.String()), slog.String("capped", maxPrice.String()))
		gasPrice = maxPrice
	}

	bytecode := common.FromHex(erc20Bytecode)
	estimatedGas, err := e.client.EstimateGas(ctx, ethereum.CallMsg{From: e.fromAddr, Data: bytecode})
	if err != nil {
		estimatedGas = 1500000
	} else {
		// 应用动态安全裕度
		// #nosec G115
		estimatedGas += (estimatedGas * uint64(e.gasSafetyMargin) / 100)
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), estimatedGas, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		return common.Address{}, err
	}

	if err := e.client.SendTransaction(ctx, signedTx); err != nil {
		_ = e.nm.ResyncNonce(ctx)
		return common.Address{}, err
	}

	receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}
	return receipt.ContractAddress, nil
}
