package emulator

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
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

func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	currentChainNonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err == nil && currentChainNonce > nm.pendingNonce {
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
type Emulator struct {
	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	fromAddr   common.Address
	contract   common.Address
	chainID    *big.Int
	nm         *NonceManager

	// 回调
	OnSelfHealing func(reason string)

	// 配置参数
	blockInterval time.Duration
	txInterval    time.Duration
	txAmount      *big.Int

	logger *slog.Logger
}

func NewEmulator(rpcURL string, privKeyHex string) (*Emulator, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

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
		txInterval:    5 * time.Second, // 演示建议 5 秒
		txAmount:      big.NewInt(100),
		logger:        engine.Logger,
	}, nil
}

// ensureBalance 演示级余额补给逻辑
func (e *Emulator) ensureBalance(ctx context.Context) error {
	balance, err := e.client.BalanceAt(ctx, e.fromAddr, nil)
	if err != nil {
		return err
	}

	// 阈值：50 ETH
	threshold := new(big.Int).Mul(big.NewInt(50), big.NewInt(1e18))
	if balance.Cmp(threshold) < 0 {
		e.logger.Info("🚨 余额不足，正在自动执行演示级补给...", slog.String("current", balance.String()))
		// 使用 Anvil 特有的 setBalance 方法
		err := e.client.Client().CallContext(ctx, nil, "anvil_setBalance", e.fromAddr, "0x3635C9ADC5DEA00000") // 1000 ETH
		if err != nil {
			return fmt.Errorf("auto_topup_failed: %w", err)
		}
		e.logger.Info("✅ 余额补给成功！", slog.String("address", e.fromAddr.Hex()))
	}
	return nil
}

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

	bytecode := common.FromHex(erc20Bytecode)
	estimatedGas, err := e.client.EstimateGas(ctx, ethereum.CallMsg{From: e.fromAddr, Data: bytecode})
	if err != nil {
		estimatedGas = 1500000
	} else {
		estimatedGas = estimatedGas + (estimatedGas / 5)
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), estimatedGas, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		return common.Address{}, err
	}

	if err := e.client.SendTransaction(ctx, signedTx); err != nil {
		e.nm.ResyncNonce(ctx)
		return common.Address{}, err
	}

	receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}
	return receipt.ContractAddress, nil
}

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

	// 演示级随机金额生成 (1-100)
	randomVal, _ := rand.Int(rand.Reader, big.NewInt(100))
	transferVal := new(big.Int).Add(randomVal, big.NewInt(1))

	methodID := common.FromHex("0xa9059cbb")
	// 演示级：随机生成接收地址，增加视觉丰富度
	randomAddrBytes := make([]byte, 20)
	rand.Read(randomAddrBytes)
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
		estimatedGas = estimatedGas + (estimatedGas / 5)
	}

	tx := types.NewTransaction(nonce, e.contract, big.NewInt(0), estimatedGas, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		return
	}

	if err := e.client.SendTransaction(ctx, signedTx); err != nil {
		e.logger.Error("send_failed", slog.String("error", err.Error()))
		// ---------------- 自修复逻辑 ----------------
		// 如果发现 nonce 错误（通常是由于环境重置或漂移），立即强制重同步
		if strings.Contains(err.Error(), "nonce too low") || strings.Contains(err.Error(), "already known") {
			e.logger.Warn("🚨 NONCE_OUT_OF_SYNC_DETECTED", slog.String("action", "immediate_resync"))
			if e.OnSelfHealing != nil {
				e.OnSelfHealing("nonce_mismatch")
			}
			e.nm.ResyncNonce(ctx)
		} else {
			e.nm.ResyncNonce(ctx) // 其他错误也尝试重同步以保持稳健
		}
		// -------------------------------------------
		return
	}

	e.logger.Info("📤 [Emulator] Sent REAL Transfer",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.String("to", targetAddr.Hex()),
		slog.String("amount", transferVal.String()),
		slog.Uint64("nonce", nonce),
	)

	go func() {
		receipt, err := e.waitForReceipt(ctx, signedTx.Hash())
		if err == nil {
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

// erc20Bytecode 现在是动态的：它会读取 calldata 中的 amount 和 to 地址，并正确触发 Transfer 事件
// 逻辑：
// 1. CALLDATALOAD(36) -> Amount, 存入 MSTORE(0)
// 2. CALLDATALOAD(4) -> To Topic
// 3. CALLER -> From Topic
// 4. LOG3(0, 32, TransferTopic, From, To)
const erc20Bytecode = "603180600b6000396000f3602435600052600435337fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60206000a300"
