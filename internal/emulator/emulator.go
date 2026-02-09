package emulator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// NonceManager 负责管理账户的 Nonce，确保高频发送下的顺序性与一致性
type NonceManager struct {
	client       *ethclient.Client
	address      common.Address
	mu           sync.Mutex
	pendingNonce uint64
	logger       *slog.Logger
}

func NewNonceManager(client *ethclient.Client, addr common.Address, logger *slog.Logger) (*NonceManager, error) {
	nonce, err := client.PendingNonceAt(context.Background(), addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get initial nonce: %w", err)
	}
	return &NonceManager{
		client:       client,
		address:      addr,
		pendingNonce: nonce,
		logger:       logger,
	}, nil
}

// GetNextNonce 获取并递增 Nonce
func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// 每次获取前，先检查链上实际状态，防止外部干扰或本地缓存漂移
	currentChainNonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err != nil {
		return 0, err
	}

	if currentChainNonce > nm.pendingNonce {
		nm.logger.Warn("nonce_drift_detected_fixing",
			slog.Uint64("local", nm.pendingNonce),
			slog.Uint64("chain", currentChainNonce),
		)
		nm.pendingNonce = currentChainNonce
	}

	nonce := nm.pendingNonce
	nm.pendingNonce++
	return nonce, nil
}

// ResyncNonce 强制从链上同步 Nonce
func (nm *NonceManager) ResyncNonce(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err != nil {
		return err
	}
	nm.pendingNonce = nonce
	nm.logger.Info("nonce_resynced", slog.Uint64("new_nonce", nonce))
	return nil
}

// Emulator 是内置的流量生成引擎
// 它自动部署 ERC20 合约并定期发送转账交易
type Emulator struct {
	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	fromAddr   common.Address
	contract   common.Address
	chainID    *big.Int
	nm         *NonceManager

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

	nm, err := NewNonceManager(client, fromAddr, engine.Logger)
	if err != nil {
		return nil, err
	}

	return &Emulator{
		client:        client,
		privateKey:    privKey,
		fromAddr:      fromAddr,
		chainID:       chainID,
		nm:            nm,
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

	// 0. 显式资金储备
	err := e.client.Client().CallContext(ctx, nil, "anvil_setBalance", e.fromAddr, "0x3635C9ADC5DEA00000") // 1000 ETH
	if err != nil {
		e.logger.Warn("failed_to_set_anvil_balance", slog.String("error", err.Error()))
	} else {
		e.logger.Info("deployer_account_funded", slog.String("address", e.fromAddr.Hex()))
	}

	// 1. 自动部署合约
	deployCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	contractAddr, err := e.deployContract(deployCtx)
	cancel()
	if err != nil {
		e.logger.Error("contract_deployment_failed", slog.String("error", err.Error()))
		return err
	}
	e.contract = contractAddr
	e.logger.Info("contract_deployed", slog.String("address", contractAddr.Hex()))

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
			// Anvil auto-mines
		case <-txTicker.C:
			e.sendTransfer(ctx)
		}
	}
}

// deployContract 部署一个简单的 ERC20 合约
func (e *Emulator) deployContract(ctx context.Context) (common.Address, error) {
	nonce, err := e.nm.GetNextNonce(ctx)
	if err != nil {
		return common.Address{}, err
	}

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Address{}, err
	}

	bytecode := common.FromHex(erc20Bytecode)

	// 动态估算部署 Gas
	estimatedGas, err := e.client.EstimateGas(ctx, ethereum.CallMsg{
		From: e.fromAddr,
		Data: bytecode,
	})
	if err != nil {
		e.logger.Warn("gas_estimation_failed_using_default", slog.String("error", err.Error()))
		estimatedGas = 1500000 // Fallback
	} else {
		// 增加 20% 安全裕度
		estimatedGas = estimatedGas + (estimatedGas / 5)
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), estimatedGas, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		return common.Address{}, err
	}

	err = e.client.SendTransaction(ctx, signedTx)
	if err != nil {
		e.nm.ResyncNonce(ctx) // 发送失败需重同步 Nonce
		return common.Address{}, err
	}

	receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}

	return receipt.ContractAddress, nil
}

// sendTransfer 发送 ERC20 转账交易
func (e *Emulator) sendTransfer(ctx context.Context) {
	nonce, err := e.nm.GetNextNonce(ctx)
	if err != nil {
		e.logger.Error("failed_to_get_nonce", slog.String("error", err.Error()))
		return
	}

	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		e.logger.Error("failed_to_get_gas_price", slog.String("error", err.Error()))
		return
	}

	methodID := common.FromHex("0xa9059cbb")
	targetAddr := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	toAddr := common.LeftPadBytes(targetAddr.Bytes(), 32)
	amount := common.LeftPadBytes(e.txAmount.Bytes(), 32)

	var data []byte
	data = append(data, methodID...)
	data = append(data, toAddr...)
	data = append(data, amount...)

	// 动态估算转账 Gas
	estimatedGas, err := e.client.EstimateGas(ctx, ethereum.CallMsg{
		From: e.fromAddr,
		To:   &e.contract,
		Data: data,
	})
	if err != nil {
		e.logger.Warn("transfer_gas_estimation_failed_using_default", slog.String("error", err.Error()))
		estimatedGas = 100000 // Fallback
	} else {
		estimatedGas = estimatedGas + (estimatedGas / 5) // 20% 裕度
	}

	tx := types.NewTransaction(nonce, e.contract, big.NewInt(0), estimatedGas, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		e.logger.Error("failed_to_sign_transfer_tx", slog.String("error", err.Error()))
		return
	}

	err = e.client.SendTransaction(ctx, signedTx)
	if err != nil {
		e.logger.Error("failed_to_send_transfer_tx", slog.String("error", err.Error()))
		e.nm.ResyncNonce(ctx)
		return
	}

	e.logger.Info("📤 [Emulator] Sent REAL Transfer",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.Uint64("nonce", nonce),
		slog.Uint64("gas_limit", estimatedGas),
	)

	go func() {
		receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
		if err != nil {
			e.logger.Error("❌ [Emulator] Confirmation timeout", slog.String("tx_hash", signedTx.Hash().Hex()))
		} else {
			e.logger.Info("✅ [Emulator] Confirmed in block",
				slog.String("tx_hash", signedTx.Hash().Hex()),
				slog.Uint64("block", receipt.BlockNumber.Uint64()),
			)
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

const erc20Bytecode = "604380600b6000396000f36103e86000527370997970C51812dc3A010C7d01b50e0d17dc79C8337fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60206000a300"