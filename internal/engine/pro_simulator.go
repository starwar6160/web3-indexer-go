package engine

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type simAccount struct {
	addr  common.Address
	pk    string // 仅用于本地 Anvil 模拟，实际应从配置读取
	nonce uint64
	mu    sync.Mutex
}

// ProSimulator 工业级混沌发生器
type ProSimulator struct {
	rpcURL   string
	enabled  bool
	tps      int
	ctx      context.Context
	cancel   context.CancelFunc
	tokens   []TokenInfo
	accounts []*simAccount
	client   *ethclient.Client
}

func NewProSimulator(rpcURL string, enabled bool, tps int) *ProSimulator {
	ctx, cancel := context.WithCancel(context.Background())
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		slog.Error("failed_to_dial_rpc_for_pro_simulator", "err", err)
	}

	// Anvil 默认前 3 个账户
	pks := []string{
		"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
		"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
	}
	addrs := []string{
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		"0x70997970C51812dc3A010C7d01b50e0d17dc79ee",
		"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
	}

	simAccs := make([]*simAccount, len(addrs))
	for i := range addrs {
		simAccs[i] = &simAccount{
			addr: common.HexToAddress(addrs[i]),
			pk:   pks[i],
		}
	}

	return &ProSimulator{
		rpcURL:  rpcURL,
		enabled: enabled,
		tps:     tps,
		ctx:     ctx,
		cancel:  cancel,
		client:  client,
		tokens: []TokenInfo{
			{common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), "USDC", 6, 1.0},
			{common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), "USDT", 6, 1.0},
			{common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), "WBTC", 8, 45000.0},
			{common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"), "WETH", 18, 3000.0},
		},
		accounts: simAccs,
	}
}

func (s *ProSimulator) Start() {
	if !s.enabled || s.client == nil {
		return
	}

	// 初始化 Nonce
	for _, acc := range s.accounts {
		n, err := s.client.NonceAt(s.ctx, acc.addr, nil)
		if err == nil {
			acc.nonce = n
		}
	}

	slog.Info("🚀 [CHAOS_ENGINE] Ignition successful", "tps", s.tps, "workers", len(s.accounts))

	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(s.tps))
		defer ticker.Stop()

		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				acc := s.accounts[secureIntn(len(s.accounts))]
				go s.executeChaosAction(acc)
			}
		}
	}()
}

func (s *ProSimulator) executeChaosAction(acc *simAccount) {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	action := secureIntn(100)
	var err error
	var typeLabel string

	switch {
	case action < 15:
		err = s.deployDummy(acc)
		typeLabel = "🏗️ DEPLOY"
	case action < 40:
		err = s.sendETH(acc)
		typeLabel = "⛽ ETH"
	case action < 60:
		err = s.approve(acc)
		typeLabel = "🔓 APPROVE"
	default:
		err = s.transferERC20(acc)
		typeLabel = "💸 TRANSFER"
	}

	if err == nil {
		acc.nonce++
		// 立即挖矿确保零延迟显示
		if mineErr := s.mine(); mineErr != nil {
			slog.Debug("mine_failed", "err", mineErr)
		}
		if secureIntn(10) == 0 {
			slog.Debug("🔥 chaos_injection_success", "type", typeLabel, "from", acc.addr.Hex()[:10])
		}
	} else if strings.Contains(err.Error(), "nonce too low") {
		// 如果 nonce 过低，重放同步一次
		n, err := s.client.NonceAt(s.ctx, acc.addr, nil)
		if err == nil {
			acc.nonce = n
		}
	}
}

func (s *ProSimulator) sendETH(acc *simAccount) error {
	to := s.accounts[secureIntn(len(s.accounts))].addr
	amount := new(big.Int).Mul(big.NewInt(int64(1+secureIntn(10))), big.NewInt(1e15)) // 0.001 - 0.01 ETH
	tx := types.NewTransaction(acc.nonce, to, amount, 21000, big.NewInt(1e9), nil)
	return s.signAndSend(tx, acc.pk)
}

func (s *ProSimulator) transferERC20(acc *simAccount) error {
	token := s.tokens[secureIntn(len(s.tokens))]
	to := s.accounts[secureIntn(len(s.accounts))].addr

	// 生成 visible amount (10 - 500 tokens)
	amount := s.formatAmount(float64(10+secureIntn(490)), token.Decimals)

	methodID := common.FromHex("0xa9059cbb")
	data := make([]byte, 0, 68)
	data = append(data, methodID...)
	data = append(data, common.LeftPadBytes(to.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)

	tx := types.NewTransaction(acc.nonce, token.Address, big.NewInt(0), 100000, big.NewInt(1e9), data)
	return s.signAndSend(tx, acc.pk)
}

func (s *ProSimulator) approve(acc *simAccount) error {
	token := s.tokens[secureIntn(len(s.tokens))]
	spender := s.accounts[secureIntn(len(s.accounts))].addr

	methodID := common.FromHex("0x095ea7b3")
	data := make([]byte, 0, 68)
	data = append(data, methodID...)
	data = append(data, common.LeftPadBytes(spender.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(common.MaxHash.Bytes(), 32)...)

	tx := types.NewTransaction(acc.nonce, token.Address, big.NewInt(0), 100000, big.NewInt(1e9), data)
	return s.signAndSend(tx, acc.pk)
}

func (s *ProSimulator) deployDummy(acc *simAccount) error {
	// 极简合约 bytecode
	data := common.FromHex("6080604052348015600f57600080fd5b50603e80601d6000396000f3fe6080604052600080fd")
	tx := types.NewContractCreation(acc.nonce, big.NewInt(0), 500000, big.NewInt(1e9), data)
	return s.signAndSend(tx, acc.pk)
}

func (s *ProSimulator) signAndSend(tx *types.Transaction, pkHex string) error {
	privKey, err := crypto.HexToECDSA(pkHex)
	if err != nil {
		return err
	}
	chainID, err := s.client.ChainID(s.ctx)
	if err != nil {
		return err
	}
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privKey)
	if err != nil {
		return err
	}
	return s.client.SendTransaction(s.ctx, signedTx)
}

func (s *ProSimulator) mine() error {
	return s.client.Client().CallContext(s.ctx, nil, "anvil_mine", "0x1")
}

func (s *ProSimulator) formatAmount(amount float64, decimals int) *big.Int {
	multiplier := new(big.Float).SetFloat64(amount)
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	multiplier.Mul(multiplier, precision)
	result := new(big.Int)
	multiplier.Int(result)
	return result
}

func (s *ProSimulator) Stop() { s.cancel() }
