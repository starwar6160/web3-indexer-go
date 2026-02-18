package engine

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// DeFiSimulator 工业级 DeFi 交易模拟器
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

	// 模拟的代币
	tokens []*TokenInfo

	// 模拟的套利机器人
	arbitrageBots []common.Address

	tps             int
	batchSize       int
	complexityLevel string
}

type TokenInfo struct {
	Address  common.Address
	Symbol   string
	Decimals int
	PriceUSD float64
}

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
		uniswapV3Router: common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
		curvePool:       common.HexToAddress("0xbEbc44782C7dB0a1A60Cb6fe97d0b483032FF1C7"),
		balancerVault:   common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"),
		aaveV3Pool:      common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2"),
		tokens: []*TokenInfo{
			{common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), "USDC", 6, 1.0},
			{common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), "USDT", 6, 1.0},
			{common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), "WBTC", 8, 45000.0},
			{common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"), "WETH", 18, 3000.0},
			{common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"), "DAI", 18, 1.0},
		},
		arbitrageBots: []common.Address{
			common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0"),
			common.HexToAddress("0x5615dEb798BB3E4dFa01397d0Db2C6b0404A38D7"),
			common.HexToAddress("0x3f5CE5FBFe3E9af3971dD833D26bA9b5C936f0bE"),
		},
		tps:             10,
		batchSize:       5,
		complexityLevel: "complex",
	}

	return simulator, nil
}

func (s *DeFiSimulator) Start(injectChan chan<- *SynthesizedTransfer) {
	if !s.enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(s.tps))
		defer ticker.Stop()

		batchCount := 0
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
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

func (s *DeFiSimulator) Stop() { s.cancel() }
