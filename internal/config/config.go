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
	DatabaseURL        string
	RPCURLs            []string // 支持多个RPC URL
	WSSURL             string
	ChainID            int64
	StartBlock         int64
	StartBlockStr      string // String representation to handle "latest"
	LogLevel           string
	LogFormat          string
	RPCTimeout         time.Duration // RPC超时配置
	RPCRateLimit       int           // 每秒允许的RPC请求数 (RPS)
	FetchConcurrency   int           // 并发抓取数
	FetchBatchSize     int           // 批量处理大小
	MaxGasPrice        int64         // 模拟器允许的最大 Gas Price (单位: Gwei)
	GasSafetyMargin    int           // Gas Limit 的安全裕度百分比 (默认 20)
	CheckpointBatch    int           // 多少个区块更新一次数据库检查点 (默认 100)
	RetryQueueSize     int           // 失败任务重试队列的大小 (默认 500)
	DemoMode           bool          // 是否开启演示模式
	EnableSimulator    bool          // 是否开启模拟交易生成器
	NetworkMode        string        // 网络模式: anvil, sepolia, mainnet
	IsTestnet          bool          // 是否为测试网模式
	MaxSyncBatch       int           // 最大同步批次大小（用于控制请求频率）
	EnableEnergySaving bool          // 是否开启节能模式（懒惰模式）
	EnableRecording    bool          // 🚀 新增：是否开启 LZ4 录制
	RecordingPath      string        // 🚀 新增：录制文件路径

	// 🛡️ Deadlock watchdog config
	DeadlockWatchdogEnabled   bool  // 死锁看门狗开关
	DeadlockStallThresholdSec int64 // 闲置阈值（秒）
	DeadlockCheckIntervalSec  int64 // 检查间隔（秒）

	// 🔥 Anvil Lab Mode config
	ForceAlwaysActive bool // 强制禁用休眠（实验室环境）

	// 📐 Height verification config (advanced_metrics)
	StrictHeightCheck bool  // 当 Synced > On-Chain 时触发警告并强制刷新
	DriftTolerance    int64 // 允许 indexedHead 超过 chainHead 的最大块数（RPC 节点传播延迟容忍）

	// 代币过滤配置
	WatchedTokenAddresses []string // 监控的 ERC20 合约地址
	TokenFilterMode       string   // "whitelist" 或 "all"
	Port                  string
	AppTitle              string
}

func Load() *Config {
	// 🚀 工业级增强：递归寻找 .env 文件，解决从不同子目录启动时的路径问题
	if err := godotenv.Load(); err != nil {
		if err := godotenv.Load("../.env"); err != nil {
			if err := godotenv.Load("../../.env"); err != nil {
				log.Printf("Note: .env file not found in current or parent directories")
			}
		}
	}

	const trueVal = "true"

	// 明确模式
	demoMode := strings.ToLower(os.Getenv("DEMO_MODE")) == trueVal || strings.ToLower(os.Getenv("EMULATOR_ENABLED")) == trueVal
	energySaving := strings.ToLower(os.Getenv("ENABLE_ENERGY_SAVING")) == trueVal
	chainID := getEnvAsInt64("CHAIN_ID", 1)
	networkMode := strings.ToLower(getEnv("NETWORK_MODE", "mainnet"))

	// 解析模拟器开关
	enableSimulatorStr := os.Getenv("ENABLE_SIMULATOR")
	var enableSimulator bool
	if enableSimulatorStr != "" {
		enableSimulator = strings.EqualFold(enableSimulatorStr, trueVal)
	} else {
		// 默认逻辑：Demo 模式或本地 Anvil 自动开启
		enableSimulator = demoMode || chainID == 31337
	}

	// 🛡️ 物理隔绝锁：非 Anvil 模式下强制禁止模拟器
	if networkMode != "anvil" && chainID != 31337 {
		if enableSimulator {
			log.Printf("🔒 SECURITY_LOCK: NetworkMode=%s detected. Forcing ENABLE_SIMULATOR=false", networkMode)
			enableSimulator = false
		}
	}

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
	maxSyncBatch := int(getEnvAsInt64("MAX_SYNC_BATCH", 20)) // 提高至 20 块，对抗 1.0 TPS 限制

	// 🛡️ Deadlock watchdog 配置
	deadlockWatchdogEnabled := strings.ToLower(os.Getenv("DEADLOCK_WATCHDOG_ENABLED")) == trueVal
	deadlockStallThresholdSec := getEnvAsInt64("DEADLOCK_STALL_THRESHOLD_SECONDS", 120)
	deadlockCheckIntervalSec := getEnvAsInt64("DEADLOCK_CHECK_INTERVAL_SECONDS", 30)

	// 🔥 Anvil Lab Mode 配置
	forceAlwaysActive := strings.ToLower(os.Getenv("FORCE_ALWAYS_ACTIVE")) == trueVal

	// Check if we're connecting to a testnet
	isTestnet := false
	for _, url := range rpcUrls {
		if strings.Contains(strings.ToLower(url), "sepolia") ||
			strings.Contains(strings.ToLower(url), "holesky") ||
			strings.Contains(strings.ToLower(url), "goerli") {
			isTestnet = true
			break
		}
	}

	// Handle START_BLOCK with special "latest" keyword
	startBlockStr := getEnv("START_BLOCK", "0")
	var startBlock int64

	if startBlockStr == "latest" {
		startBlock = -1 // Special value to indicate "latest" - will be resolved at runtime
	} else {
		startBlock = getEnvAsInt64("START_BLOCK", 0)
	}

	// 解析监控的代币地址
	watchedTokensStr := getEnv("WATCHED_TOKEN_ADDRESSES", "")
	var watchedTokens []string
	if watchedTokensStr != "" {
		watchedTokens = strings.Split(watchedTokensStr, ",")
		for i, addr := range watchedTokens {
			watchedTokens[i] = strings.TrimSpace(addr)
		}
	}

	cfg := &Config{
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable"),
		RPCURLs:            rpcUrls,
		WSSURL:             getEnv("WSS_URL", ""),
		ChainID:            chainID,
		StartBlock:         startBlock,
		StartBlockStr:      startBlockStr,
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		LogFormat:          getEnv("LOG_FORMAT", "json"),
		RPCTimeout:         time.Duration(rpcTimeoutSeconds) * time.Second,
		RPCRateLimit:       rpcRateLimit,
		FetchConcurrency:   fetchConcurrency,
		FetchBatchSize:     fetchBatchSize,
		MaxGasPrice:        maxGasPrice,
		GasSafetyMargin:    gasSafetyMargin,
		CheckpointBatch:    checkpointBatch,
		RetryQueueSize:     retryQueueSize,
		DemoMode:           demoMode,
		EnableSimulator:    enableSimulator,
		NetworkMode:        networkMode,
		IsTestnet:          isTestnet,
		MaxSyncBatch:       maxSyncBatch,
		EnableEnergySaving: energySaving,
		EnableRecording:    strings.ToLower(os.Getenv("ENABLE_RECORDING")) == trueVal,
		RecordingPath:      getEnv("RECORDING_PATH", "trajectory.lz4"),
		// 🛡️ Deadlock watchdog: enabled for all networks
		DeadlockWatchdogEnabled:   deadlockWatchdogEnabled,
		DeadlockStallThresholdSec: deadlockStallThresholdSec,
		DeadlockCheckIntervalSec:  deadlockCheckIntervalSec,
		// 🔥 Anvil Lab Mode
		ForceAlwaysActive: forceAlwaysActive,
		StrictHeightCheck:  strings.ToLower(os.Getenv("STRICT_HEIGHT_CHECK")) != "false", // default true
		DriftTolerance:     getEnvAsInt64("DRIFT_TOLERANCE", 5),
		WatchedTokenAddresses:     watchedTokens,
		TokenFilterMode:           getEnv("TOKEN_FILTER_MODE", "whitelist"), // 默认启用过滤
		Port:                      getEnv("PORT", "8080"),
		AppTitle:                  getEnv("APP_TITLE", "🚀 Web3 Indexer Dashboard"),
	}

	// 🚨 优先级锁定：优先信任显式传入的 RPC_URLS 环境变量
	if os.Getenv("RPC_URLS") == "" && cfg.DemoMode {
		cfg.RPCURLs = []string{"http://127.0.0.1:8545"}
		cfg.ChainID = 31337
		cfg.RPCRateLimit = 200 // 本地环境，火力全开
		log.Printf("🔒 SECURITY_LOCK: NO RPC_URLS FOUND, FALLING BACK TO LOCAL ANVIL (targets=%v)", cfg.RPCURLs)
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
