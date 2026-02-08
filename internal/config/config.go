package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	RPCURLs     []string // 支持多个RPC URL
	WSSURL      string
	ChainID     int64
	StartBlock  int64
	LogLevel    string
	LogFormat   string
	RPCTimeout  time.Duration // RPC超时配置
}

func Load() *Config {
	_ = godotenv.Load() // .env文件是可选的

	// 解析RPC URL列表（支持逗号分隔）
	rpcUrlsStr := getEnv("RPC_URLS", "https://eth.llamarpc.com")
	rpcUrls := strings.Split(rpcUrlsStr, ",")
	for i, url := range rpcUrls {
		rpcUrls[i] = strings.TrimSpace(url)
	}

	// 解析RPC超时（默认10秒）
	rpcTimeoutSeconds := getEnvAsInt64("RPC_TIMEOUT_SECONDS", 10)

	return &Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable"),
		RPCURLs:     rpcUrls,
		WSSURL:      getEnv("WSS_URL", ""),
		ChainID:     getEnvAsInt64("CHAIN_ID", 1),
		StartBlock:  getEnvAsInt64("START_BLOCK", 10000000), // 默认从1000万开始，避免从0同步
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		LogFormat:   getEnv("LOG_FORMAT", "json"),
		RPCTimeout:  time.Duration(rpcTimeoutSeconds) * time.Second,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		log.Printf("Invalid %s: %s, using default %d", key, valueStr, defaultValue)
		return defaultValue
	}
	return value
}
