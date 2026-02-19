package engine

import (
	"log/slog"
	"strings"
)

// IndexerPolicy 环境感知的索引器策略配置
type IndexerPolicy struct {
	AllowSleep     bool // 是否允许休眠（Eco-Mode）
	EnforceQuota   bool // 是否强制配额限制
	BurstBatchSize int  // 批处理大小
	LabMode        bool // 实验室模式（无限火力）
}

// GetPolicy 根据 RPC URL 自动检测环境并返回最优策略
func GetPolicy(rpcURLs []string, chainID int64) IndexerPolicy {
	// 优先级 1: ChainID 显式检测
	if chainID == 31337 {
		return IndexerPolicy{
			AllowSleep:     false, // 永远不睡
			EnforceQuota:   false, // 无视配额
			BurstBatchSize: 100,   // 本地加满马力
			LabMode:        true,
		}
	}

	// 优先级 2: RPC URL 特征检测
	for _, url := range rpcURLs {
		if isLocalAnvil(url) {
			slog.Info("🔥 Anvil environment detected", "url", url)
			return IndexerPolicy{
				AllowSleep:     false,
				EnforceQuota:   false,
				BurstBatchSize: 100,
				LabMode:        true,
			}
		}
	}

	// 默认: 生产环境保守策略
	return IndexerPolicy{
		AllowSleep:     true, // 允许 Eco-Mode
		EnforceQuota:   true, // 强制配额限制
		BurstBatchSize: 20,   // 保守批次
		LabMode:        false,
	}
}

// IsLocalAnvil 检测是否为本地 Anvil 环境（导出供其他包使用）
func IsLocalAnvil(rpcURL string) bool {
	lowerURL := strings.ToLower(rpcURL)
	anvilSignals := []string{
		"localhost",
		"127.0.0.1",
		"anvil",
		":8545",
		":8092",
	}

	for _, signal := range anvilSignals {
		if strings.Contains(lowerURL, signal) {
			return true
		}
	}
	return false
}

// isLocalAnvil 内部使用的别名（保持向后兼容）
func isLocalAnvil(rpcURL string) bool {
	return IsLocalAnvil(rpcURL)
}
