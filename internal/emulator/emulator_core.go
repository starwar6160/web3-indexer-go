package emulator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Metrics 记录仿真器运行状态
type Metrics struct {
	Sent       atomic.Uint64
	Confirmed  atomic.Uint64
	Failed     atomic.Uint64
	SelfHealed atomic.Uint64
}

// MetricsSnapshot is a snapshot of metrics that can be safely passed to callbacks
type MetricsSnapshot struct {
	Sent       uint64
	Confirmed  uint64
	Failed     uint64
	SelfHealed uint64
}

// Emulator 是内置的流量生成引擎
type Emulator struct {
	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	fromAddr   common.Address
	contract   common.Address
	chainID    *big.Int
	nm         *NonceManager
	Metrics    Metrics

	// 回调
	OnSelfHealing func(reason string)
	OnMetrics     func(m MetricsSnapshot)

	// 配置参数
	txAmount        *big.Int
	maxGasPrice     int64 // 最大允许的 Gas Price (Gwei)
	gasSafetyMargin int   // Gas Limit 安全裕度 (%)
	blockInterval   time.Duration
	txInterval      time.Duration

	logger *slog.Logger
}

func NewEmulator(rpcURL, privKeyHex string, opts ...func(*Emulator)) (*Emulator, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	privKeyHex = strings.TrimPrefix(privKeyHex, "0x")
	privKey, err := crypto.HexToECDSA(privKeyHex) // Using the crypto package directly
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey) // Using the crypto package directly
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	nm, err := NewNonceManager(client, fromAddr, slog.Default())
	if err != nil {
		return nil, err
	}

	emu := &Emulator{
		client:          client,
		privateKey:      privKey,
		fromAddr:        fromAddr,
		chainID:         chainID,
		nm:              nm,
		blockInterval:   3 * time.Second,
		txInterval:      5 * time.Second, // 演示建议 5 秒
		txAmount:        big.NewInt(100),
		maxGasPrice:     500, // 默认 500 Gwei
		gasSafetyMargin: 20,  // 默认 20%
		logger:          slog.Default(),
	}
	for _, opt := range opts {
		opt(emu)
	}
	return emu, nil
}

// WithTxInterval 设置交易发送间隔（函数式选项）
func WithTxInterval(d time.Duration) func(*Emulator) {
	return func(e *Emulator) {
		if d > 0 {
			e.txInterval = d
		}
	}
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

// erc20Bytecode 现在是动态的：它会读取 calldata 中的 amount 和 to 地址，并正确触发 Transfer 事件
// 逻辑：
// 1. CALLDATALOAD(36) -> Amount, 存入 MSTORE(0)
// 2. CALLDATALOAD(4) -> To Topic
// 3. CALLER -> From Topic
// 4. LOG3(0, 32, TransferTopic, From, To)
const erc20Bytecode = "603180600b6000396000f3602435600052600435337fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60206000a300"
