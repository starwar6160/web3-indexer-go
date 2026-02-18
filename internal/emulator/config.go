package emulator

import (
	"os"
	"strings"
	"time"
)

// Config holds emulator configuration.
// #nosec G117 - PrivateKey is exported for YAML/JSON configuration loading
// It should be loaded from environment variables in production, not hardcoded
type Config struct {
	Enabled       bool
	PrivateKey    string // Exported for config file support, use env vars in production
	TxAmount      string
	RPCURL        string
	BlockInterval time.Duration
	TxInterval    time.Duration
}

// LoadConfig loads emulator configuration from environment variables.
func LoadConfig() Config {
	enabled := strings.ToLower(os.Getenv("EMULATOR_ENABLED")) == "true"

	blockIntervalStr := os.Getenv("EMULATOR_BLOCK_INTERVAL")
	if blockIntervalStr == "" {
		blockIntervalStr = "3s" // 默认出块时间
	}
	blockInterval, err := time.ParseDuration(blockIntervalStr)
	if err != nil {
		blockInterval = 3 * time.Second
	}

	txIntervalStr := os.Getenv("EMULATOR_TX_INTERVAL")
	if txIntervalStr == "" {
		txIntervalStr = "5s" // 默认交易频率
	}
	txInterval, err := time.ParseDuration(txIntervalStr)
	if err != nil {
		txInterval = 5 * time.Second
	}

	// Anvil 默认账户 0 的私钥（仅用于本地演示，公开已知）
	privKey := os.Getenv("EMULATOR_PRIVATE_KEY")
	if privKey == "" && enabled {
		privKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	}

	return Config{
		Enabled:       enabled,
		RPCURL:        os.Getenv("EMULATOR_RPC_URL"),
		PrivateKey:    privKey,
		BlockInterval: blockInterval,
		TxInterval:    txInterval,
		TxAmount:      os.Getenv("EMULATOR_TX_AMOUNT"),
	}
}

// IsValid checks if the configuration is valid.
func (c Config) IsValid() bool {
	return c.RPCURL != "" && c.PrivateKey != ""
}
