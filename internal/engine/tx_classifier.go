package engine

import (
	"encoding/hex"
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
	Protocol string // "Uniswap V2", "Uniswap V3", "SushiSwap", etc.
	Action   string // "swap", "mint", "burn", "stake", etc.
	Amount   string // 解析的金额（如果有）
	TokenIn  string // 输入代币
	TokenOut string // 输出代币
}

// 常见 DeFi 协议的 Method ID（前 4 字节）
var knownProtocolSelectors = map[string]DeFiProtocolDetail{
	// Uniswap V2
	"0x38ed1739": {Protocol: "Uniswap V2", Action: "swapExactTokensForTokens"},
	"0x8803dbee": {Protocol: "Uniswap V2", Action: "swapTokensForExactTokens"},
	"0x7ff36ab5": {Protocol: "Uniswap V2", Action: "swapExactETHForTokens"},
	"0x18cbafe5": {Protocol: "Uniswap V2", Action: "swapExactTokensForETH"},
	
	// Uniswap V3
	"0xc04b8d59": {Protocol: "Uniswap V3", Action: "exactInputSingle"},
	"0xdb3e2198": {Protocol: "Uniswap V3", Action: "exactInput"},
	"0x414bf389": {Protocol: "Uniswap V3", Action: "exactOutputSingle"},
	
	// SushiSwap
	"0xd3052939": {Protocol: "SushiSwap", Action: "swapExactTokensForTokens"},
	
	// Aave (Lending)
	"0x4515cef4": {Protocol: "Aave", Action: "deposit"},
	"0x59e860a4": {Protocol: "Aave", Action: "withdraw"},
	"0x4aa4a4fc": {Protocol: "Aave", Action: "borrow"},
	"0xa391344c": {Protocol: "Aave", Action: "repay"},
	
	// NFT (ERC-721)
	"0x23b872dd": {Protocol: "ERC-721", Action: "transferFrom"},
	"0x42842e0e": {Protocol: "ERC-721", Action: "safeTransferFrom"},
	"0xa22cb465": {Protocol: "ERC-721", Action: "setApprovalForAll"},
	
	// NFT (ERC-1155)
	"0xf242432a": {Protocol: "ERC-1155", Action: "safeTransferFrom"},
	"0x2eb2c2d6": {Protocol: "ERC-1155", Action: "safeBatchTransferFrom"},
}

// ClassifyTransaction 交易精细化分类器
// 核心能力：在不调用额外 RPC 的情况下，仅凭交易数据识别类型
func ClassifyTransaction(to string, value string, input string, hasLogs bool) TransactionCategory {
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
		
		// 检查是否为已知协议
		if detail, ok := knownProtocolSelectors[selector]; ok {
			// 根据协议类型返回分类
			switch {
			case strings.Contains(detail.Protocol, "Uniswap") || 
			     strings.Contains(detail.Protocol, "Sushi"):
				slog.Debug("🧩 DEX Swap Detected", 
					"protocol", detail.Protocol, 
					"action", detail.Action,
					"selector", selector)
				return TxCategoryDEXSwap
				
			case strings.Contains(detail.Protocol, "Aave"):
				slog.Debug("🏦 DeFi Lending Detected",
					"protocol", detail.Protocol,
					"action", detail.Action,
					"selector", selector)
				return TxCategoryDeFiLending
				
			case strings.Contains(detail.Protocol, "ERC-721"):
				if detail.Action == "transferFrom" || detail.Action == "safeTransferFrom" {
					// NFT 转账（非 0x0）
					return TxCategoryNFTTransfer
				}
				return TxCategorySmartContractCall
				
			case strings.Contains(detail.Protocol, "ERC-1155"):
				if detail.Action == "safeTransferFrom" || detail.Action == "safeBatchTransferFrom" {
					return TxCategoryNFTTransfer
				}
				return TxCategorySmartContractCall
				
			default:
				return TxCategorySmartContractCall
			}
		}
	}
	
	// 4. 检查是否为 NFT Mint（通过日志判断）
	if hasLogs {
		// 这里可以进一步分析日志，识别 NFT Mint
		// 简化版：如果有 Transfer 事件且 from 是 0x0
		// 实际实施需要解析 Logs
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
