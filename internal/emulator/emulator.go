package emulator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Emulator 是内置的流量生成引擎
// 它自动部署 ERC20 合约并定期发送转账交易
type Emulator struct {
	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	fromAddr   common.Address
	contract   common.Address
	chainID    *big.Int
	nonce      uint64

	// 配置参数
	blockInterval time.Duration // 触发新区块的间隔
	txInterval    time.Duration // 发送交易的间隔
	txAmount      *big.Int      // 每笔转账的金额

	logger *slog.Logger
}

// NewEmulator 创建一个新的仿真器实例
func NewEmulator(rpcURL string, privKeyHex string) (*Emulator, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	// 解析私钥
	privKeyHex = strings.TrimPrefix(privKeyHex, "0x")
	privKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	return &Emulator{
		client:        client,
		privateKey:    privKey,
		fromAddr:      fromAddr,
		chainID:       chainID,
		blockInterval: 3 * time.Second,
		txInterval:    8 * time.Second,
		txAmount:      big.NewInt(1000),
		logger:        engine.Logger,
	}, nil
}

// Start 启动仿真引擎
// 它会自动部署合约，然后定期发送交易
func (e *Emulator) Start(ctx context.Context, addressChan chan<- common.Address) error {
	e.logger.Info("emulator_starting",
		slog.String("from_address", e.fromAddr.Hex()),
		slog.String("chain_id", e.chainID.String()),
	)

	// 0. 显式资金储备 (V1 Lesson 5)
	// 给 deployer 充值 1000 ETH 以确保 Gas 充足
	err := e.client.Client().CallContext(ctx, nil, "anvil_setBalance", e.fromAddr, "0x3635C9ADC5DEA00000") // 1000 ETH
	if err != nil {
		e.logger.Warn("failed_to_set_anvil_balance", slog.String("error", err.Error()))
		// 继续运行，可能已经有余额了
	} else {
		e.logger.Info("deployer_account_funded", slog.String("address", e.fromAddr.Hex()))
	}

	// 1. 自动部署合约
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	contractAddr, err := e.deployContract(deployCtx)
	cancel()
	if err != nil {
		e.logger.Error("contract_deployment_failed",
			slog.String("error", err.Error()),
		)
		return err
	}
	e.contract = contractAddr
	e.logger.Info("contract_deployed",
		slog.String("address", contractAddr.Hex()),
	)

	// 2. 将新地址发送给 Indexer 自动配置
	if addressChan != nil {
		select {
		case addressChan <- contractAddr:
			e.logger.Info("contract_address_sent_to_indexer")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// 3. 启动定时器循环
	blockTicker := time.NewTicker(e.blockInterval)
	txTicker := time.NewTicker(e.txInterval)
	defer blockTicker.Stop()
	defer txTicker.Stop()

	e.logger.Info("emulator_loop_started",
		slog.String("block_interval", e.blockInterval.String()),
		slog.String("tx_interval", e.txInterval.String()),
	)

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("emulator_stopped")
			return ctx.Err()
		case <-blockTicker.C:
			// e.triggerNewBlock(ctx) // Anvil auto-mines with --block-time 1
		case <-txTicker.C:
			e.sendTransfer(ctx)
		}
	}
}

// deployContract 部署一个简单的 ERC20 合约
func (e *Emulator) deployContract(ctx context.Context) (common.Address, error) {
	nonce, err := e.client.PendingNonceAt(ctx, e.fromAddr)
	if err != nil {
		return common.Address{}, err
	}
	e.nonce = nonce

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Address{}, err
	}

	// 极简 ERC20 合约字节码 (包含 Transfer 事件)
	bytecode := common.FromHex(erc20Bytecode)

	tx := types.NewContractCreation(nonce, big.NewInt(0), 1000000, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		return common.Address{}, err
	}

	err = e.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Address{}, err
	}

	// 等待交易确认
	receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}

	e.nonce++
	return receipt.ContractAddress, nil
}

// triggerNewBlock 通过发送一笔 ETH 交易来触发新区块
func (e *Emulator) triggerNewBlock(ctx context.Context) {
	nonce, err := e.client.PendingNonceAt(ctx, e.fromAddr)
	if err != nil {
		e.logger.Error("failed_to_get_nonce",
			slog.String("error", err.Error()),
		)
		return
	}

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		e.logger.Error("failed_to_get_gas_price",
			slog.String("error", err.Error()),
		)
		return
	}

	// 发送 1 wei 到一个固定地址来触发新区块
	toAddress := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	tx := types.NewTransaction(nonce, toAddress, big.NewInt(1), 21000, gasPrice, nil)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		e.logger.Error("failed_to_sign_block_trigger_tx",
			slog.String("error", err.Error()),
		)
		return
	}

	err = e.client.SendTransaction(ctx, signedTx)
	if err != nil {
		e.logger.Error("failed_to_send_block_trigger_tx",
			slog.String("error", err.Error()),
		)
		return
	}

	e.logger.Debug("block_trigger_sent",
		slog.String("tx_hash", signedTx.Hash().Hex()),
	)
}

// sendTransfer 发送 ERC20 转账交易
func (e *Emulator) sendTransfer(ctx context.Context) {
	nonce, err := e.client.PendingNonceAt(ctx, e.fromAddr)
	if err != nil {
		e.logger.Error("failed_to_get_nonce_for_transfer",
			slog.String("stage", "EMULATOR"),
			slog.String("error", err.Error()),
		)
		return
	}

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		e.logger.Error("failed_to_get_gas_price_for_transfer",
			slog.String("stage", "EMULATOR"),
			slog.String("error", err.Error()),
		)
		return
	}

	// 构建 ERC20 transfer(address,uint256) 的调用数据
	// transfer 的方法 ID 是 0xa9059cbb
	methodID := common.FromHex("0xa9059cbb")
	toAddr := common.LeftPadBytes(
		common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8").Bytes(),
		32,
	)
	amount := common.LeftPadBytes(e.txAmount.Bytes(), 32)

	var data []byte
	data = append(data, methodID...)
	data = append(data, toAddr...)
	data = append(data, amount...)

	tx := types.NewTransaction(nonce, e.contract, big.NewInt(0), 500000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		e.logger.Error("failed_to_sign_transfer_tx",
			slog.String("stage", "EMULATOR"),
			slog.String("error", err.Error()),
		)
		return
	}

	e.logger.Info("📡 仿真器：正在发送交易...",
		slog.String("stage", "EMULATOR"),
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.String("to_contract", e.contract.Hex()),
	)

	err = e.client.SendTransaction(ctx, signedTx)
	if err != nil {
		e.logger.Error("failed_to_send_transfer_tx",
			slog.String("stage", "EMULATOR"),
			slog.String("error", err.Error()),
		)
		return
	}

	// 异步等待确认，不阻塞主循环
	go func() {
		receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
		if err != nil {
			e.logger.Error("❌ 仿真器：交易未在限时内确认",
				slog.String("stage", "EMULATOR"),
				slog.String("tx_hash", signedTx.Hash().Hex()),
				slog.String("error", err.Error()),
			)
		} else {
			e.logger.Info("✅ 仿真器：交易已物理确认入块",
				slog.String("stage", "EMULATOR"),
				slog.String("tx_hash", signedTx.Hash().Hex()),
				slog.Uint64("block", receipt.BlockNumber.Uint64()),
				slog.Uint64("status", receipt.Status),
				slog.Int("logs_count", len(receipt.Logs)),
			)
			if receipt.Status == 0 {
				e.logger.Error("transfer_reverted",
					slog.String("stage", "EMULATOR"),
					slog.String("tx_hash", signedTx.Hash().Hex()),
				)
			}
		}
	}()
}

// waitForReceipt 等待交易确认（最多 30 秒）
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
				return nil, fmt.Errorf("timeout waiting for receipt %s", hash.Hex())
			}
		}
	}
}

// SetBlockInterval 设置触发新区块的间隔
func (e *Emulator) SetBlockInterval(interval time.Duration) {
	e.blockInterval = interval
}

// SetTxInterval 设置发送交易的间隔
func (e *Emulator) SetTxInterval(interval time.Duration) {
	e.txInterval = interval
}

// SetTxAmount 设置每笔转账的金额
func (e *Emulator) SetTxAmount(amount *big.Int) {
	e.txAmount = amount
}

// GetContractAddress 返回部署的合约地址
func (e *Emulator) GetContractAddress() common.Address {
	return e.contract
}

// ERC20 合约字节码（极简实现，包含 Transfer 事件）
// "Always Emit" bytecode: any call emits Transfer(caller, 0x7099..79C8, 1000)
// Runtime: PUSH2 1000, PUSH1 0, MSTORE, PUSH20 to, CALLER, PUSH32 TransferHash, PUSH1 32, PUSH1 0, LOG3, STOP
// Init: PUSH1 67, DUP1, PUSH1 11, PUSH1 0, CODECOPY, PUSH1 0, RETURN
const erc20Bytecode = "604380600b6000396000f36103e86000527370997970C51812dc3A010C7d01b50e0d17dc79C8337fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60206000a300"
