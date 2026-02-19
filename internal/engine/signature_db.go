package engine

// 本地函数签名库（4-Byte Selector Database）
// 设计理念：用本地 CPU 算力换取昂贵的 RPC 额度
// 16G 内存足够支撑包含数万条记录的哈希表

var localSignatureDB = map[string]FunctionSignature{
	// ========== ERC-20 Standard ==========
	"0x095ea7b3": {Name: "approve", Protocol: "ERC-20", Desc: "Approve spending"},
	"0x70a08231": {Name: "balanceOf", Protocol: "ERC-20", Desc: "Get balance"},
	"0xa9059cbb": {Name: "transfer", Protocol: "ERC-20", Desc: "Transfer tokens"},
	"0x23b872dd": {Name: "transferFrom", Protocol: "ERC-20", Desc: "Transfer from"},

	// ========== ERC-721 NFT ==========
	"0x42842e0e": {Name: "safeTransferFrom", Protocol: "ERC-721", Desc: "Safe transfer"},
	"0xa22cb465": {Name: "setApprovalForAll", Protocol: "ERC-721", Desc: "Set approval"},
	"0xe985e9c5": {Name: "totalSupply", Protocol: "ERC-721", Desc: "Total supply"},

	// ========== Uniswap V2 ==========
	"0x38ed1739": {Name: "swapExactTokensForTokens", Protocol: "Uniswap V2", Desc: "Swap tokens"},
	"0x8803dbee": {Name: "swapTokensForExactTokens", Protocol: "Uniswap V2", Desc: "Swap tokens exact"},
	"0x7ff36ab5": {Name: "swapExactETHForTokens", Protocol: "Uniswap V2", Desc: "Swap ETH for tokens"},
	"0x18cbafe5": {Name: "swapExactTokensForETH", Protocol: "Uniswap V2", Desc: "Swap tokens for ETH"},
	"0x2f7bb4f7": {Name: "swapETHForExactTokens", Protocol: "Uniswap V2", Desc: "Swap ETH exact"},
	"0x4a25d94a": {Name: "swapTokensForExactETH", Protocol: "Uniswap V2", Desc: "Swap tokens exact ETH"},
	"0xded9382a": {Name: "swapExactTokensForTokensSupportingFeeOnTransferTokens", Protocol: "Uniswap V2", Desc: "Swap with fee"},
	"0xfb3bdb41": {Name: "swapExactETHForTokensSupportingFeeOnTransferTokens", Protocol: "Uniswap V2", Desc: "Swap ETH with fee"},
	"0x7f369353": {Name: "swapExactTokensForTokensSupportingFeeOnTransferTokens", Protocol: "Uniswap V2", Desc: "Swap tokens with fee"},
	"0xb6f9de95": {Name: "swapExactTokensForETHSupportingFeeOnTransferTokens", Protocol: "Uniswap V2", Desc: "Swap tokens for ETH with fee"},

	// ========== Uniswap V3 ==========
	"0xc04b8d59": {Name: "exactInputSingle", Protocol: "Uniswap V3", Desc: "Exact input single"},
	"0xdb3e2198": {Name: "exactInput", Protocol: "Uniswap V3", Desc: "Exact input"},
	"0x414bf389": {Name: "exactOutputSingle", Protocol: "Uniswap V3", Desc: "Exact output single"},
	"0x09b81346": {Name: "exactOutput", Protocol: "Uniswap V3", Desc: "Exact output"},
	"0xa257c30d": {Name: "multicall", Protocol: "Uniswap V3", Desc: "Multicall"},

	// ========== SushiSwap ==========
	"0xd3052939": {Name: "swapExactTokensForTokens", Protocol: "SushiSwap", Desc: "Swap tokens"},
	"0x44f7149a": {Name: "swapTokensForExactTokens", Protocol: "SushiSwap", Desc: "Swap tokens exact"},
	"0x1b6f8a6c": {Name: "swapExactETHForTokens", Protocol: "SushiSwap", Desc: "Swap ETH for tokens"},
	"0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640": {Name: "swapTokensForExactETH", Protocol: "SushiSwap", Desc: "Swap tokens for ETH (alt selector)"},

	// ========== Aave (Lending) ==========
	"0x4515cef4": {Name: "deposit", Protocol: "Aave", Desc: "Deposit collateral"},
	"0x59e860a4": {Name: "withdraw", Protocol: "Aave", Desc: "Withdraw collateral"},
	"0x4aa4a4fc": {Name: "borrow", Protocol: "Aave", Desc: "Borrow asset"},
	"0xa391344c": {Name: "repay", Protocol: "Aave", Desc: "Repay borrow"},
	"0xd9e5462e": {Name: "liquidationCall", Protocol: "Aave", Desc: "Liquidation call"},
	"0x8a05c0c8": {Name: "setUserUseReserveAsCollateral", Protocol: "Aave", Desc: "Set collateral"},
	"0xccee4390": {Name: "swapBorrowRateMode", Protocol: "Aave", Desc: "Swap rate mode"},

	// ========== Compound ==========
	"0xc5ebeaec": {Name: "supply", Protocol: "Compound", Desc: "Supply collateral"},
	"0x443a1aca": {Name: "redeem", Protocol: "Compound", Desc: "Redeem collateral"},
	"0x8e8c6c41": {Name: "borrow", Protocol: "Compound", Desc: "Borrow asset"},
	"0x0dTLd8d1": {Name: "repayBorrow", Protocol: "Compound", Desc: "Repay borrow"},

	// ========== OpenSea (Seaport) ==========
	"0x800adc34": {Name: "fulfillAdvancedOrder", Protocol: "Seaport", Desc: "Fulfill order"},
	"0x00b202cc": {Name: "fulfillOrder", Protocol: "Seaport", Desc: "Fulfill basic order"},

	// ========== Chainlink ==========
	"0x855afb1d": {Name: "requestOracleData", Protocol: "Chainlink", Desc: "Request oracle"},
	"0xf1c1cd2d": {Name: "cancelOracleRequest", Protocol: "Chainlink", Desc: "Cancel request"},

	// ========== Multi-sig ==========
	"0x6a761202": {Name: "submitTransaction", Protocol: "Gnosis Safe", Desc: "Submit tx"},
	"0x8562b13d": {Name: "confirmTransaction", Protocol: "Gnosis Safe", Desc: "Confirm tx"},
	"0xbd76f0d2": {Name: "executeTransaction", Protocol: "Gnosis Safe", Desc: "Execute tx"},

	// ========== Proxy ==========
	"0x8f282fbf": {Name: "upgradeTo", Protocol: "TransparentProxy", Desc: "Upgrade impl"},
	"0x00f714ce": {Name: "upgradeToAndCall", Protocol: "TransparentProxy", Desc: "Upgrade and call"},
}

// FunctionSignature 函数签名信息
type FunctionSignature struct {
	Name     string // 函数名称
	Protocol string // 协议名称
	Desc     string // 描述
}

// QuickIdentify 快速识别函数（零 RPC 消耗，极速 CPU 匹配）
func QuickIdentify(input string) *FunctionSignature {
	if len(input) < 10 {
		return nil
	}

	selector := input[:10]
	if sig, ok := localSignatureDB[selector]; ok {
		return &sig
	}

	return nil
}

// GetSignatureCount 获取本地签名库大小
func GetSignatureCount() int {
	return len(localSignatureDB)
}
