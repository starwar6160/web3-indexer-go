package emulator

import (
	"os"
	"strings"
	"time"
)

// Config holds emulator configuration
type Config struct {
	Enabled       bool
	RpcURL        string
	PrivateKey    string
	BlockInterval time.Duration
	TxInterval    time.Duration
	TxAmount      string
}

// LoadConfig loads emulator configuration from environment variables
func LoadConfig() Config {
	enabled := strings.ToLower(os.Getenv("EMULATOR_ENABLED")) == "true"

	blockIntervalStr := os.Getenv("EMULATOR_BLOCK_INTERVAL")
	if blockIntervalStr == "" {
		blockIntervalStr = "3s"
	}
	blockInterval, _ := time.ParseDuration(blockIntervalStr)

	txIntervalStr := os.Getenv("EMULATOR_TX_INTERVAL")
	if txIntervalStr == "" {
		txIntervalStr = "8s"
	}
	txInterval, _ := time.ParseDuration(txIntervalStr)

	return Config{
		Enabled:       enabled,
		RpcURL:        os.Getenv("EMULATOR_RPC_URL"),
		PrivateKey:    os.Getenv("EMULATOR_PRIVATE_KEY"),
		BlockInterval: blockInterval,
		TxInterval:    txInterval,
		TxAmount:      os.Getenv("EMULATOR_TX_AMOUNT"),
	}
}

// IsValid checks if the configuration is valid
func (c Config) IsValid() bool {
	return c.RpcURL != "" && c.PrivateKey != ""
}
