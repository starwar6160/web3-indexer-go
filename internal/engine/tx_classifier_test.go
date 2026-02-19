package engine

import (
	"testing"
)

// TestIntegration_DeepParsing_AI_Friendly 验证深度解析正确性
func TestIntegration_DeepParsing_AI_Friendly(t *testing.T) {
	tests := []struct {
		name          string
		to            string
		value         string
		input         string
		hasLogs       bool
		expected      TransactionCategory
		expectedProto string
	}{
		{
			name:          "Uniswap V2 Swap",
			to:            "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D",
			value:         "0",
			input:         "0x38ed17390000000000000000000000000000000000000000000000000000000000001400000000000000000000000000000000000000000000000000000000000000000",
			hasLogs:       false,
			expected:      TxCategoryDEXSwap,
			expectedProto: "Uniswap V2",
		},
		{
			name:          "Uniswap V3 Swap",
			to:            "0xE592427A0AEce92De3Edee1F18E0157C05861564",
			value:         "1000000000000000",
			input:         "0xc04b8d5900000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
			hasLogs:       false,
			expected:      TxCategoryDEXSwap,
			expectedProto: "Uniswap V3",
		},
		{
			name:          "Aave Deposit",
			to:            "0x7d2768dE32b0b80b7a3454c06BdAc94A69DDc7A9",
			value:         "0",
			input:         "0x4515cef4000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
			hasLogs:       false,
			expected:      TxCategoryDeFiLending,
			expectedProto: "Aave",
		},
		{
			name:          "Contract Deploy",
			to:            "",
			value:         "0",
			input:         "0x6080604052348015600f57600080fd5b50603f80601e539160005...",
			hasLogs:       false,
			expected:      TxCategoryContractDeploy,
			expectedProto: "",
		},
		{
			name:          "ETH Transfer",
			to:            "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
			value:         "1000000000000000000",
			input:         "0x",
			hasLogs:       false,
			expected:      TxCategoryETHTransfer,
			expectedProto: "",
		},
		{
			name:          "NFT Transfer (ERC-721)",
			to:            "0xC36442b4a4522E871399Cd717aBDD847Ab11FE88",
			value:         "0",
			input:         "0x23b872dd0000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000000003",
			hasLogs:       false,
			expected:      TxCategoryNFTTransfer,
			expectedProto: "ERC-721",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyTransaction(tt.to, tt.value, tt.input, tt.hasLogs)

			if result != tt.expected {
				t.Fatalf("AI_FIX_REQUIRED: Classification failed for %s. Expected %s, got %s",
					tt.name, tt.expected, result)
			}

			// 验证协议识别
			if tt.expectedProto != "" {
				detail := ParseDeFiProtocolDetail(tt.input)
				if detail == nil {
					t.Fatalf("AI_FIX_REQUIRED: Failed to parse protocol for %s", tt.name)
				}
				if detail.Protocol != tt.expectedProto {
					t.Errorf("AI_FIX_REQUIRED: Protocol mismatch. Expected %s, got %s",
						tt.expectedProto, detail.Protocol)
				}
			}

			t.Logf("✅ SUCCESS: %s correctly identified as %s", tt.name, result)
		})
	}
}

// TestClassifier_GetCategoryIcon 验证图标映射
func TestClassifier_GetCategoryIcon(t *testing.T) {
	tests := []struct {
		category     TransactionCategory
		expectedIcon string
	}{
		{TxCategoryDEXSwap, "💧"},
		{TxCategoryNFTMint, "🎨"},
		{TxCategoryNFTTransfer, "🖼️"},
		{TxCategoryDeFiLending, "🏦"},
		{TxCategoryContractDeploy, "🏗️"},
		{TxCategoryETHTransfer, "💸"},
	}

	for _, tt := range tests {
		result := GetCategoryIcon(tt.category)
		if result != tt.expectedIcon {
			t.Errorf("AI_FIX_REQUIRED: Icon mismatch. Expected %s, got %s",
				tt.expectedIcon, result)
		}
	}

	t.Logf("✅ SUCCESS: All category icons correct")
}
