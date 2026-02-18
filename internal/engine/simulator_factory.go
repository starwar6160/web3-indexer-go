package engine

import (
	"context"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// SyntheticTransferInjector 合成数据注入器
// 在 Anvil 空链模式下，生成模拟的 Transfer 事件以验证系统功能
type SyntheticTransferInjector struct {
	client    *ethclient.Client
	chainID   *big.Int
	enabled   bool
	rateLimit time.Duration // 注入速率
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc

	// 模拟的代币合约地址（仅用于显示，不需要真实部署）
	mockTokens []common.Address
	// 模拟的钱包地址
	mockWallets []common.Address
}

// NewSyntheticTransferInjector 创建合成数据注入器
func NewSyntheticTransferInjector(rpcURL string, chainID *big.Int, enabled bool) (*SyntheticTransferInjector, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	injector := &SyntheticTransferInjector{
		client:    client,
		chainID:   chainID,
		enabled:   enabled,
		rateLimit: 500 * time.Millisecond, // 默认每秒 2 笔
		ctx:       ctx,
		cancel:    cancel,
		mockTokens: []common.Address{
			common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), // Mock USDC
			common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), // Mock USDT
			common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), // Mock WBTC
		},
		mockWallets: []common.Address{
			common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79ee"), // Anvil Account #1
			common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"), // Anvil Account #2
			common.HexToAddress("0x90F79bf6EB2c4f870365E785982E1f101E93b906"), // Anvil Account #3
			common.HexToAddress("0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65"), // Anvil Account #4
		},
	}

	slog.Info("🏭 Synthetic Transfer Injector initialized",
		"enabled", enabled,
		"rate_limit", injector.rateLimit,
		"mock_tokens", len(injector.mockTokens),
		"mock_wallets", len(injector.mockWallets))

	return injector, nil
}

// Start 启动合成数据注入循环
func (s *SyntheticTransferInjector) Start(injectChan chan<- *SynthesizedTransfer) {
	if !s.enabled {
		slog.Info("🏭 Synthetic Transfer Injector disabled (not Anvil mode)")
		return
	}

	slog.Info("🚀 Starting Synthetic Transfer Injector",
		"rate", s.rateLimit,
		"target", "1 transfer every "+s.rateLimit.String())

	go func() {
		ticker := time.NewTicker(s.rateLimit)
		defer ticker.Stop()

		seqNum := int64(0)
		for {
			select {
			case <-s.ctx.Done():
				slog.Info("🛑 Synthetic Transfer Injector stopped")
				return
			case <-ticker.C:
				transfer := s.generateMockTransfer(seqNum)
				if transfer != nil {
					injectChan <- transfer
					seqNum++
				}
			}
		}
	}()
}

// Stop 停止注入器
func (s *SyntheticTransferInjector) Stop() {
	s.cancel()
}

// generateMockTransfer 生成模拟的 Transfer 事件
func (s *SyntheticTransferInjector) generateMockTransfer(seqNum int64) *SynthesizedTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 随机选择代币和钱包
	token := s.mockTokens[secureIntn(len(s.mockTokens))]
	from := s.mockWallets[secureIntn(len(s.mockWallets))]
	to := s.mockWallets[secureIntn(len(s.mockWallets))]

	// 确保发送者和接收者不同
	for to == from {
		to = s.mockWallets[secureIntn(len(s.mockWallets))]
	}

	// 随机金额 (1-1000 tokens)
	amount := big.NewInt(int64(secureIntn(1000) + 1))
	amount.Mul(amount, big.NewInt(1000000000)) // 模拟 18 位精度

	// 获取当前区块号
	currentBlock, err := s.client.HeaderByNumber(s.ctx, nil)
	if err != nil {
		slog.Error("Failed to get current block for mock transfer", "err", err)
		return nil
	}

	transfer := &SynthesizedTransfer{
		// #nosec G115 - Synthetic hash generation for test/Anvil mode
		TxHash:       common.BytesToHash([]byte{0x00, byte(seqNum)}), // 伪造交易哈希
		BlockNumber:  currentBlock.Number.Uint64(),
		BlockHash:    currentBlock.Hash(),
		TokenAddress: token,
		From:         from,
		To:           to,
		Amount:       amount,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}

	return transfer
}

// SetRateLimit 动态调整注入速率
func (s *SyntheticTransferInjector) SetRateLimit(dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rateLimit = dur
	slog.Info("🏭 Injection rate updated", "new_rate", dur)
}

// SynthesizedTransfer 合成的 Transfer 事件
type SynthesizedTransfer struct {
	TxHash       common.Hash
	BlockNumber  uint64
	BlockHash    common.Hash
	TokenAddress common.Address
	From         common.Address
	To           common.Address
	Amount       *big.Int
	Timestamp    int64
	Synthesized  bool // 标记为合成数据
}
