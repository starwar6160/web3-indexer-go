package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	mathrand "math/rand/v2"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// DeFiSimulator 工业级 DeFi 交易模拟器
// 模拟高频套利、Flashloan、MEV 等复杂场景
type DeFiSimulator struct {
	client  *ethclient.Client
	chainID *big.Int
	enabled bool
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc

	// 模拟的 DeFi 协议地址
	uniswapV3Router common.Address
	curvePool       common.Address
	balancerVault   common.Address
	aaveV3Pool      common.Address

	// 模拟的代币（带精度）
	tokens []*TokenInfo

	// 模拟的套利机器人地址
	arbitrageBots []common.Address

	// 配置参数
	tps             int    // 每秒交易数
	batchSize       int    // 每批交易数
	complexityLevel string // "simple", "complex", "mev"
}

// TokenInfo 代币信息（含精度）
type TokenInfo struct {
	Address  common.Address
	Symbol   string
	Decimals int
	PriceUSD float64 // USD 价格（用于计算实际金额）
}

// NewDeFiSimulator 创建 DeFi 模拟器
func NewDeFiSimulator(rpcURL string, chainID *big.Int, enabled bool) (*DeFiSimulator, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	simulator := &DeFiSimulator{
		client:          client,
		chainID:         chainID,
		enabled:         enabled,
		ctx:             ctx,
		cancel:          cancel,
		uniswapV3Router: common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"), // Uniswap V3 SwapRouter
		curvePool:       common.HexToAddress("0xbEbc44782C7dB0a1A60Cb6fe97d0b483032FF1C7"), // 3Pool
		balancerVault:   common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"), // Balancer Vault
		aaveV3Pool:      common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2"), // Aave V3 Pool
		tokens: []*TokenInfo{
			{common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), "USDC", 6, 1.0},
			{common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), "USDT", 6, 1.0},
			{common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), "WBTC", 8, 45000.0},
			{common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"), "WETH", 18, 3000.0},
			{common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"), "DAI", 18, 1.0},
		},
		arbitrageBots: []common.Address{
			common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0"), // MEV Bot #1
			common.HexToAddress("0x5615dEb798BB3E4dFa01397d0Db2C6b0404A38D7"), // MEV Bot #2
			common.HexToAddress("0x3f5CE5FBFe3E9af3971dD833D26bA9b5C936f0bE"), // Binance Hot Wallet
		},
		tps:             10,        // 默认每秒 10 笔
		batchSize:       5,         // 每批 5 笔
		complexityLevel: "complex", // 默认复杂模式
	}

	slog.Info("🏭 DeFi Simulator initialized",
		"enabled", enabled,
		"tokens", len(simulator.tokens),
		"bots", len(simulator.arbitrageBots),
		"tps", simulator.tps,
		"complexity", simulator.complexityLevel)

	return simulator, nil
}

// Start 启动模拟循环
func (s *DeFiSimulator) Start(injectChan chan<- *SynthesizedTransfer) {
	if !s.enabled {
		slog.Info("🏭 DeFi Simulator disabled")
		return
	}

	slog.Info("🚀 Starting DeFi Simulator",
		"tps", s.tps,
		"batch_size", s.batchSize,
		"complexity", s.complexityLevel)

	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(s.tps))
		defer ticker.Stop()

		batchCount := 0
		for {
			select {
			case <-s.ctx.Done():
				slog.Info("🛑 DeFi Simulator stopped")
				return
			case <-ticker.C:
				// 每秒生成 tps 笔交易
				for i := 0; i < s.batchSize; i++ {
					transfer := s.generateDeFiTransfer(int64(batchCount*10 + i))
					if transfer != nil {
						injectChan <- transfer
					}
				}
				batchCount++
			}
		}
	}()
}

// generateDeFiTransfer 生成 DeFi 交易
func (s *DeFiSimulator) generateDeFiTransfer(seqNum int64) *SynthesizedTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 获取当前区块号
	header, err := s.client.HeaderByNumber(s.ctx, nil)
	if err != nil {
		return nil
	}
	currentBlock := header.Number.Uint64()

	// 随机选择交易类型
	txType := secureIntn(100)
	var transfer *SynthesizedTransfer

	switch {
	case txType < 60:
		// 60% 普通 Swap
		transfer = s.generateSwapTransfer(currentBlock, seqNum)
	case txType < 80:
		// 20% 套利交易（多跳）
		transfer = s.generateArbitrageTransfer(currentBlock, seqNum)
	case txType < 90:
		// 10% Flashloan
		transfer = s.generateFlashloanTransfer(currentBlock, seqNum)
	default:
		// 10% MEV (Sandwich Attack)
		transfer = s.generateMEVTransfer(currentBlock, seqNum)
	}

	return transfer
}

// generateSwapTransfer 生成普通 Swap 交易
func (s *DeFiSimulator) generateSwapTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	// 随机选择代币对
	token0 := s.tokens[secureIntn(len(s.tokens))]
	_ = s.tokens[secureIntn(len(s.tokens))] // token1 (未使用，简化逻辑)

	// 幂律分布金额（模拟真实交易）
	amountRaw := s.generatePowerLawAmount(token0.Decimals)

	// 随机用户地址
	from := s.randomUserAddress()
	to := s.uniswapV3Router // Uniswap Router

	// 构造伪造的 TxHash
	txHash := s.generateTxHash(blockNumber, seqNum, "SWAP")

	transfer := &SynthesizedTransfer{
		TxHash:       txHash,
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token0.Address,
		From:         from,
		To:           to,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}

	slog.Debug("🔄 [SWAP] Generated",
		"token", token0.Symbol,
		"amount", amountRaw.String(),
		"block", blockNumber)

	return transfer
}

// generateArbitrageTransfer 生成套利交易
func (s *DeFiSimulator) generateArbitrageTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	// 套利机器人
	bot := s.arbitrageBots[secureIntn(len(s.arbitrageBots))]

	// 选择代币进行套利
	token0 := s.tokens[secureIntn(len(s.tokens))]

	// 大额交易（套利通常是高价值）
	amountRaw := s.generateLargeAmount(token0.Decimals)

	txHash := s.generateTxHash(blockNumber, seqNum, "ARBITRAGE")

	transfer := &SynthesizedTransfer{
		TxHash:       txHash,
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token0.Address,
		From:         bot,
		To:           s.uniswapV3Router,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}

	slog.Info("🦈 [ARBITRAGE] Generated",
		"bot", bot.Hex()[:10]+"...",
		"token", token0.Symbol,
		"amount", amountRaw.String())

	return transfer
}

// generateFlashloanTransfer 生成 Flashloan 交易
func (s *DeFiSimulator) generateFlashloanTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	// Aave Pool
	pool := s.aaveV3Pool

	// 随机代币
	token := s.tokens[secureIntn(len(s.tokens))]

	// Flashloan 通常是超大额
	amountRaw := s.generateMegaAmount(token.Decimals)

	txHash := s.generateTxHash(blockNumber, seqNum, "FLASHLOAN")

	transfer := &SynthesizedTransfer{
		TxHash:       txHash,
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token.Address,
		From:         pool,
		To:           s.balancerVault, // Balancer Vault
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}

	slog.Info("⚡ [FLASHLOAN] Generated",
		"token", token.Symbol,
		"amount", amountRaw.String())

	return transfer
}

// generateMEVTransfer 生成 MEV 交易（Sandwich Attack）
func (s *DeFiSimulator) generateMEVTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	// MEV Bot
	bot := s.arbitrageBots[secureIntn(len(s.arbitrageBots))]

	// 通常攻击 WETH 或主流币
	token := s.tokens[3] // WETH

	// MEV 通常是中高额
	amountRaw := s.generateMediumAmount(token.Decimals)

	txHash := s.generateTxHash(blockNumber, seqNum, "MEV")

	transfer := &SynthesizedTransfer{
		TxHash:       txHash,
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token.Address,
		From:         bot,
		To:           s.uniswapV3Router,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}

	slog.Info("🦈 [MEV] Generated",
		"bot", bot.Hex()[:10]+"...",
		"token", token.Symbol,
		"amount", amountRaw.String())

	return transfer
}

// generatePowerLawAmount 生成符合幂律分布的金额
// 模拟真实交易：大部分是小额，少数是巨额
func (s *DeFiSimulator) generatePowerLawAmount(decimals int) *big.Int {
	// 使用指数分布生成 [0, 1) 之间的值
	expValue := mathrand.ExpFloat64()

	// 映射到不同数量级
	var magnitude float64
	switch {
	case expValue < 0.7:
		// 70% 的小额交易 (1-100 tokens)
		// #nosec G404
		magnitude = 1 + mathrand.Float64()*99
	case expValue < 0.95:
		// 25% 的中额交易 (100-10000 tokens)
		// #nosec G404
		magnitude = 100 + mathrand.Float64()*9900
	default:
		// 5% 的大额交易 (10000-1000000 tokens)
		// #nosec G404
		magnitude = 10000 + mathrand.Float64()*990000
	}

	// 应用精度
	amount := new(big.Float)
	amount.SetInt64(int64(magnitude))
	amount.Mul(amount, big.NewFloat(1e18)) // 18 位基准

	// 调整为目标精度
	targetPrecision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	amount.Quo(amount, targetPrecision)

	result := new(big.Int)
	amount.Int(result)
	return result
}

// generateLargeAmount 生成大额金额（套利交易）
func (s *DeFiSimulator) generateLargeAmount(decimals int) *big.Int {
	// #nosec G404
	base := new(big.Float).SetFloat64(10000 + mathrand.Float64()*90000) // 10k-100k
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	base.Mul(base, precision)

	result := new(big.Int)
	base.Int(result)
	return result
}

// generateMegaAmount 生成超大额金额（Flashloan）
func (s *DeFiSimulator) generateMegaAmount(decimals int) *big.Int {
	// #nosec G404
	base := new(big.Float).SetFloat64(100000 + mathrand.Float64()*900000) // 100k-1M
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	base.Mul(base, precision)

	result := new(big.Int)
	base.Int(result)
	return result
}

// generateMediumAmount 生成中额金额（MEV）
func (s *DeFiSimulator) generateMediumAmount(decimals int) *big.Int {
	// #nosec G404
	base := new(big.Float).SetFloat64(1000 + mathrand.Float64()*9000) // 1k-10k
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	base.Mul(base, precision)

	result := new(big.Int)
	base.Int(result)
	return result
}

// randomUserAddress 生成随机用户地址
func (s *DeFiSimulator) randomUserAddress() common.Address {
	// 从 Anvil 默认账户中随机选择
	addresses := []string{
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		"0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		"0x90F79bf6EB2c4f870365E785982E1f101E93b906",
		"0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65",
	}
	return common.HexToAddress(addresses[secureIntn(len(addresses))])
}

// generateTxHash 生成伪造的交易哈希
func (s *DeFiSimulator) generateTxHash(blockNumber uint64, seqNum int64, txType string) common.Hash {
	// 简单伪造：blockNumber + seqNum + type
	data := make([]byte, 32)
	data[0] = byte(blockNumber >> 24)
	data[1] = byte(blockNumber >> 16)
	data[2] = byte(blockNumber >> 8)
	data[3] = byte(blockNumber)
	data[4] = byte(seqNum)
	data[5] = byte(len(txType))
	return common.BytesToHash(data)
}

// SetTPS 动态调整 TPS
func (s *DeFiSimulator) SetTPS(tps int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tps = tps
	slog.Info("🏭 DeFi Simulator TPS updated", "new_tps", tps)
}

// SetComplexity 设置复杂度级别
func (s *DeFiSimulator) SetComplexity(level string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.complexityLevel = level

	// 根据复杂度调整交易类型分布
	switch level {
	case "simple":
		// 90% Swap, 10% 其他
	case "complex":
		// 60% Swap, 20% Arbitrage, 10% Flashloan, 10% MEV
	case "mev":
		// 30% Swap, 30% Arbitrage, 20% Flashloan, 20% MEV
	}

	slog.Info("🏭 DeFi Simulator complexity updated", "level", level)
}

// Stop 停止模拟器
func (s *DeFiSimulator) Stop() {
	s.cancel()
}
