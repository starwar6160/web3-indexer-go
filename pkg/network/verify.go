package network

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

// 预定义的网络 ID（常量）
const (
	MainnetChainID = 1
	SepoliaChainID = 11155111
	AnvilChainID   = 31337
	GoerliChainID  = 5
	HoleskyChainID = 17000
)

// Name 返回 Chain ID 对应的网络名称
func Name(chainID int64) string {
	switch chainID {
	case MainnetChainID:
		return "Ethereum Mainnet"
	case SepoliaChainID:
		return "Sepolia Testnet"
	case AnvilChainID:
		return "Anvil Local"
	case GoerliChainID:
		return "Goerli Testnet"
	case HoleskyChainID:
		return "Holesky Testnet"
	default:
		return fmt.Sprintf("Unknown Network (Chain ID: %d)", chainID)
	}
}

// VerifyNetwork 校验 RPC 节点的 Chain ID
// 如果与预期不符或获取失败，返回 error
func VerifyNetwork(client *ethclient.Client, expectedChainID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取 RPC 节点的真实 Chain ID
	actualChainID, err := client.ChainID(ctx)
	if err != nil {
		slog.Error("❌ 无法获取 RPC 节点的 ChainID",
			"error", err)
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	expectedName := Name(expectedChainID)
	actualName := Name(actualChainID.Int64())

	slog.Info("📡 网络校验中...",
		"expected_chain_id", expectedChainID,
		"expected_network", expectedName,
		"actual_chain_id", actualChainID.Int64(),
		"actual_network", actualName,
	)

	// 比较 Chain ID
	if actualChainID.Cmp(big.NewInt(expectedChainID)) != 0 {
		slog.Error("🛑 [SECURITY ALERT] 网络配置冲突！",
			"expected", fmt.Sprintf("%s (ID: %d)", expectedName, expectedChainID),
			"actual", fmt.Sprintf("%s (ID: %d)", actualName, actualChainID.Int64()),
			"impact", "数据库污染风险",
		)
		return fmt.Errorf("network mismatch: expected %d, got %d", expectedChainID, actualChainID.Int64())
	}

	slog.Info("✅ 网络校验通过，环境匹配",
		"network", expectedName,
		"chain_id", expectedChainID,
	)
	return nil
}
