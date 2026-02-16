package config

const (
	// USDC (USD Coin) - Circle official on Sepolia
	SepoliaUSDC = "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238"

	// DAI (Dai Stablecoin)
	SepoliaDAI = "0xff34b3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357"

	// WETH (Wrapped Ether)
	SepoliaWETH = "0x7b79995e5f793A07Bc00c21412e50Ecae098E7f9"

	// Uniswap V3 Token (示例)
	SepoliaUNI = "0xa3382DfFcA847B84592C05AB05937aE1A38623BC"
)

// DefaultWatchedTokens 返回默认监控的热门代币列表
func DefaultWatchedTokens() []string {
	return []string{
		SepoliaUSDC,
		SepoliaDAI,
		SepoliaWETH,
		SepoliaUNI,
	}
}
