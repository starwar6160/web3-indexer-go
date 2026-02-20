package engine

import (
	"log/slog"
	"strings"
)

// TransactionCategory 交易分类
type TransactionCategory string

const (
	TxCategoryUnknown           TransactionCategory = "UNKNOWN"
	TxCategoryETHTransfer       TransactionCategory = "ETH_TRANSFER"
	TxCategoryContractDeploy    TransactionCategory = "CONTRACT_DEPLOYMENT"
	TxCategorySmartContractCall TransactionCategory = "SMART_CONTRACT_CALL"
	TxCategoryDEXSwap           TransactionCategory = "DEFI_SWAP"
	TxCategoryNFTMint           TransactionCategory = "NFT_MINT"
	TxCategoryNFTTransfer       TransactionCategory = "NFT_TRANSFER"
	TxCategoryDeFiLending       TransactionCategory = "DEFI_LENDING"
	TxCategoryDeFiStaking       TransactionCategory = "DEFI_STAKING"
)

// DeFiProtocolDetail 协议详情
type DeFiProtocolDetail struct {
	Protocol string              // "Uniswap V2", "Uniswap V3", "SushiSwap", etc.
	Action   string              // "swap", "mint", "burn", "stake", etc.
	Category TransactionCategory // 预定义的分类
}

// 常见 DeFi 协议的 Method ID（前 4 字节）
var knownProtocolSelectors = map[string]DeFiProtocolDetail{
	// Uniswap V2
	"0x38ed1739": {Protocol: "Uniswap V2", Action: "swapExactTokensForTokens", Category: TxCategoryDEXSwap},
	"0x8803dbee": {Protocol: "Uniswap V2", Action: "swapTokensForExactTokens", Category: TxCategoryDEXSwap},
	"0x7ff36ab5": {Protocol: "Uniswap V2", Action: "swapExactETHForTokens", Category: TxCategoryDEXSwap},
	"0x18cbafe5": {Protocol: "Uniswap V2", Action: "swapExactTokensForETH", Category: TxCategoryDEXSwap},

	// Uniswap V3
	"0xc04b8d59": {Protocol: "Uniswap V3", Action: "exactInputSingle", Category: TxCategoryDEXSwap},
	"0xdb3e2198": {Protocol: "Uniswap V3", Action: "exactInput", Category: TxCategoryDEXSwap},
	"0x414bf389": {Protocol: "Uniswap V3", Action: "exactOutputSingle", Category: TxCategoryDEXSwap},

	// SushiSwap
	"0xd3052939": {Protocol: "SushiSwap", Action: "swapExactTokensForTokens", Category: TxCategoryDEXSwap},

	// Aave (Lending)
	"0x4515cef4": {Protocol: "Aave", Action: "deposit", Category: TxCategoryDeFiLending},
	"0x59e860a4": {Protocol: "Aave", Action: "withdraw", Category: TxCategoryDeFiLending},
	"0x4aa4a4fc": {Protocol: "Aave", Action: "borrow", Category: TxCategoryDeFiLending},
	"0xa391344c": {Protocol: "Aave", Action: "repay", Category: TxCategoryDeFiLending},

	// NFT (ERC-721)
	"0x23b872dd": {Protocol: "ERC-721", Action: "transferFrom", Category: TxCategoryNFTTransfer},
	"0x42842e0e": {Protocol: "ERC-721", Action: "safeTransferFrom", Category: TxCategoryNFTTransfer},
	"0xa22cb465": {Protocol: "ERC-721", Action: "setApprovalForAll", Category: TxCategorySmartContractCall},

	// NFT (ERC-1155)
	"0xf242432a": {Protocol: "ERC-1155", Action: "safeTransferFrom", Category: TxCategoryNFTTransfer},
	"0x2eb2c2d6": {Protocol: "ERC-1155", Action: "safeBatchTransferFrom", Category: TxCategoryNFTTransfer},
}

// ClassifyTransaction 交易精细化分类器
func ClassifyTransaction(to string, value string, input string, _ bool) TransactionCategory {
	// 1. 合约创建
	if to == "" || to == "0x" {
		return TxCategoryContractDeploy
	}

	// 2. ETH 转账（无 input 数据）
	if input == "0x" || len(input) <= 10 {
		if value != "0" && value != "" {
			return TxCategoryETHTransfer
		}
		return TxCategoryUnknown
	}

	// 3. 智能合约调用 - 解析 Method ID
	if len(input) >= 10 {
		selector := strings.ToLower(input[:10])
		if detail, ok := knownProtocolSelectors[selector]; ok {
			slog.Debug("🧩 Protocol Detected",
				"protocol", detail.Protocol,
				"action", detail.Action,
				"category", detail.Category)
			return detail.Category
		}
	}

	return TxCategorySmartContractCall
}

// ParseDeFiProtocolDetail 解析 DeFi 协议详情
func ParseDeFiProtocolDetail(input string) *DeFiProtocolDetail {
	if len(input) >= 10 {
		selector := strings.ToLower(input[:10])
		if detail, ok := knownProtocolSelectors[selector]; ok {
			return &detail
		}
	}
	return nil
}

// GetCategoryIcon 获取分类图标（用于 UI 显示）
func GetCategoryIcon(category TransactionCategory) string {
	switch category {
	case TxCategoryDEXSwap:
		return "💧"
	case TxCategoryNFTMint:
		return "🎨"
	case TxCategoryNFTTransfer:
		return "🖼️"
	case TxCategoryDeFiLending:
		return "🏦"
	case TxCategoryDeFiStaking:
		return "⛽"
	case TxCategoryContractDeploy:
		return "🏗️"
	case TxCategoryETHTransfer:
		return "💸"
	default:
		return "📝"
	}
}

// GetCategoryLabel 获取分类标签（中文）
func GetCategoryLabel(category TransactionCategory) string {
	switch category {
	case TxCategoryDEXSwap:
		return "DEX Swap"
	case TxCategoryNFTMint:
		return "NFT Mint"
	case TxCategoryNFTTransfer:
		return "NFT Transfer"
	case TxCategoryDeFiLending:
		return "DeFi Lending"
	case TxCategoryDeFiStaking:
		return "DeFi Staking"
	case TxCategoryContractDeploy:
		return "Contract Deploy"
	case TxCategoryETHTransfer:
		return "ETH Transfer"
	case TxCategorySmartContractCall:
		return "Smart Contract Call"
	default:
		return "Unknown"
	}
}
