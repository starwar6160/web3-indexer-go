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
	DatabaseURL      string
	RPCURLs          []string // 支持多个RPC URL
	WSSURL           string
	ChainID          int64
	StartBlock       int64
	LogLevel         string
	LogFormat        string
	RPCTimeout       time.Duration // RPC超时配置
	RPCRateLimit     int           // 每秒允许的RPC请求数 (RPS)
	FetchConcurrency int           // 并发抓取数
	FetchBatchSize   int           // 批量处理大小
	MaxGasPrice      int64         // 模拟器允许的最大 Gas Price (单位: Gwei)
	GasSafetyMargin  int           // Gas Limit 的安全裕度百分比 (默认 20)
	CheckpointBatch  int           // 多少个区块更新一次数据库检查点 (默认 100)
	RetryQueueSize   int           // 失败任务重试队列的大小 (默认 500)
	DemoMode         bool          // 是否开启演示模式
}

func Load() *Config {
	_ = godotenv.Load()

	// 明确演示模式
	demoMode := strings.ToLower(os.Getenv("DEMO_MODE")) == "true" || strings.ToLower(os.Getenv("EMULATOR_ENABLED")) == "true"

	// 解析RPC URL列表
	rpcUrlsStr := getEnv("RPC_URLS", "https://eth.llamarpc.com")
	rpcUrls := strings.Split(rpcUrlsStr, ",")
	for i, url := range rpcUrls {
		rpcUrls[i] = strings.TrimSpace(url)
	}

	rpcTimeoutSeconds := getEnvAsInt64("RPC_TIMEOUT_SECONDS", 10)
	rpcRateLimit := int(getEnvAsInt64("RPC_RATE_LIMIT", 20))
	fetchConcurrency := int(getEnvAsInt64("FETCH_CONCURRENCY", 10))
	fetchBatchSize := int(getEnvAsInt64("FETCH_BATCH_SIZE", 200))
	maxGasPrice := getEnvAsInt64("MAX_GAS_PRICE", 500)
	gasSafetyMargin := int(getEnvAsInt64("GAS_SAFETY_MARGIN", 20))
	checkpointBatch := int(getEnvAsInt64("CHECKPOINT_BATCH", 100))
	retryQueueSize := int(getEnvAsInt64("RETRY_QUEUE_SIZE", 500))

	cfg := &Config{
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable"),
		RPCURLs:          rpcUrls,
		WSSURL:           getEnv("WSS_URL", ""),
		ChainID:          getEnvAsInt64("CHAIN_ID", 1),
		StartBlock:       getEnvAsInt64("START_BLOCK", 10000000),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		LogFormat:        getEnv("LOG_FORMAT", "json"),
		RPCTimeout:       time.Duration(rpcTimeoutSeconds) * time.Second,
		RPCRateLimit:     rpcRateLimit,
		FetchConcurrency: fetchConcurrency,
		FetchBatchSize:   fetchBatchSize,
		MaxGasPrice:      maxGasPrice,
		GasSafetyMargin:  gasSafetyMargin,
		CheckpointBatch:  checkpointBatch,
		RetryQueueSize:   retryQueueSize,
		DemoMode:         demoMode,
	}

	// 🚨 核弹级锁定：演示模式下，物理锁死本地环境，无视所有环境变量污染
	if cfg.DemoMode {
		cfg.RPCURLs = []string{"http://127.0.0.1:8545"}
		cfg.ChainID = 31337
		cfg.RPCRateLimit = 200 // 本地环境，火力全开
		log.Printf("🔒 SECURITY_LOCK: HARD-CODED LOCAL ANVIL MODE ENABLED")
	}

	// 打印确定性启动日志
	networkName := "Mainnet"
	if cfg.ChainID == 11155111 {
		networkName = "Sepolia"
	} else if cfg.ChainID == 31337 {
		networkName = "Anvil"
	}
	log.Printf("🚀 Architecture Loaded: mode=%v network=%s rps=%d targets=%d", 
		cfg.DemoMode, networkName, cfg.RPCRateLimit, len(cfg.RPCURLs))

	return cfg
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
