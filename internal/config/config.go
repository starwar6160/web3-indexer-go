package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	RPCURL      string
	ChainID     int64
	StartBlock  int64
}

func Load() *Config {
	_ = godotenv.Load() // .env文件是可选的

	return &Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable"),
		RPCURL:      getEnv("RPC_URL", "https://eth.llamarpc.com"),
		ChainID:     getEnvAsInt64("CHAIN_ID", 1),
		StartBlock:  getEnvAsInt64("START_BLOCK", 0),
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
