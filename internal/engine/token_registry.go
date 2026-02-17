package engine

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// TokenRegistry 代币注册表（地址 -> Symbol 映射）
type TokenRegistry struct {
	mu     sync.RWMutex
	tokens map[common.Address]string
}

// NewTokenRegistry 创建代币注册表
func NewTokenRegistry() *TokenRegistry {
	return &TokenRegistry{
		tokens: map[common.Address]string{
			// 稳定币
			common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"): "USDC",
			common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"): "USDT",
			common.HexToAddress("0x4ECaBa5870353805a9F068101A40E0f32ed605C6"): "USDC.e", // USDC on Arbitrum
			common.HexToAddress("0x94a9D9AC8a22534E3FaCa9F4e7F2E2cf85d5E4C8"): "USDC (Bridged)",

			// Wrapped Assets
			common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"): "WETH",
			common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"): "WBTC",
			common.HexToAddress("0x40D16FC0246aD3160Ccc09B8D0e3D285429740Bf"): "WSTETH",
			common.HexToAddress("0xCa14007Eff0dB1f8135f4C25B34De49AB0d42766"): "WMATIC", // Polygon

			// DeFi Blue Chips
			common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"):  "DAI",
			common.HexToAddress("0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984"):  "UNI",
			common.HexToAddress("0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9c"): "LINK",
			common.HexToAddress("0xD533a949740bb3306d119CC777fa900bA034c52D"):  "CRV",

			// LSTs (Liquid Staking Tokens)
			common.HexToAddress("0xAE78736Cd615f374D08aDFc44B9E79b9bDCeEaeE"):  "rETH",
			common.HexToAddress("0x4d9F916011233A8a94F9575c019c0C24e6D2F2b2"):  "stETH",
			common.HexToAddress("0x8c1EEdC876be49809e03EB08396afbc93EB376365"): "wBETH",

			// LSTs on Sepolia
			common.HexToAddress("0xF1f3eaA94e9a809B86c91baddaC0249a919C238C"): "wsepETH",
			common.HexToAddress("0x7b79995e5f793A07Bc00c21412e50Ecae098E7f9"): "sepolINK",

			// Gas Tokens
			common.HexToAddress("0x0000000000000000000000000000000000000000"): "ETH",     // Native ETH
			common.HexToAddress("0x4200000000000000000000000000000000000006"): "WETH.op", // Optimism
		},
	}
}

// GetSymbol 获取代币符号
func (r *TokenRegistry) GetSymbol(address common.Address) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if symbol, ok := r.tokens[address]; ok {
		return symbol
	}

	// 未注册的代币，返回地址前缀
	return address.Hex()[:10] + "..."
}

// Register 手动注册代币
func (r *TokenRegistry) Register(address common.Address, symbol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[address] = symbol
}

// 全局单例
var globalTokenRegistry = NewTokenRegistry()

// GetGlobalTokenRegistry 获取全局代币注册表
func GetGlobalTokenRegistry() *TokenRegistry {
	return globalTokenRegistry
}
