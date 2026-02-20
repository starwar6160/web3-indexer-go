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
	EphemeralMode      bool          // 🔥 新增：全内存模式，不写入数据库

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
	loadDotEnv()

	const envTrue = "true"
	demoMode := isEnvTrue("DEMO_MODE") || isEnvTrue("EMULATOR_ENABLED")
	energySaving := isEnvTrue("ENABLE_ENERGY_SAVING")
	chainID := getEnvAsInt64("CHAIN_ID", 1)
	networkMode := strings.ToLower(getEnv("NETWORK_MODE", "mainnet"))

	enableSimulator := resolveSimulatorFlag(demoMode, chainID)
	enableSimulator = applySecurityLock(networkMode, chainID, enableSimulator)

	rpcUrls := parseRPCUrls(demoMode)
	isTestnet := detectTestnet(rpcUrls)
	startBlock, startBlockStr := parseStartBlock()

	cfg := &Config{
		DatabaseURL:               getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable"),
		RPCURLs:                   rpcUrls,
		WSSURL:                    getEnv("WSS_URL", ""),
		ChainID:                   chainID,
		StartBlock:                startBlock,
		StartBlockStr:             startBlockStr,
		LogLevel:                  getEnv("LOG_LEVEL", "info"),
		LogFormat:                 getEnv("LOG_FORMAT", "json"),
		RPCTimeout:                time.Duration(getEnvAsInt64("RPC_TIMEOUT_SECONDS", 10)) * time.Second,
		RPCRateLimit:              int(getEnvAsInt64("RPC_RATE_LIMIT", 20)),
		FetchConcurrency:          int(getEnvAsInt64("FETCH_CONCURRENCY", 10)),
		FetchBatchSize:            int(getEnvAsInt64("FETCH_BATCH_SIZE", 200)),
		MaxGasPrice:               getEnvAsInt64("MAX_GAS_PRICE", 500),
		GasSafetyMargin:           int(getEnvAsInt64("GAS_SAFETY_MARGIN", 20)),
		CheckpointBatch:           int(getEnvAsInt64("CHECKPOINT_BATCH", 100)),
		RetryQueueSize:            int(getEnvAsInt64("RETRY_QUEUE_SIZE", 500)),
		DemoMode:                  demoMode,
		EnableSimulator:           enableSimulator,
		NetworkMode:               networkMode,
		IsTestnet:                 isTestnet,
		MaxSyncBatch:              int(getEnvAsInt64("MAX_SYNC_BATCH", 20)),
		EnableEnergySaving:        energySaving,
		EnableRecording:           isEnvTrue("ENABLE_RECORDING"),
		RecordingPath:             getEnv("RECORDING_PATH", "trajectory.lz4"),
		EphemeralMode:             isEnvTrue("EPHEMERAL_MODE"),
		DeadlockWatchdogEnabled:   isEnvTrue("DEADLOCK_WATCHDOG_ENABLED"),
		DeadlockStallThresholdSec: getEnvAsInt64("DEADLOCK_STALL_THRESHOLD_SECONDS", 120),
		DeadlockCheckIntervalSec:  getEnvAsInt64("DEADLOCK_CHECK_INTERVAL_SECONDS", 30),
		ForceAlwaysActive:         isEnvTrue("FORCE_ALWAYS_ACTIVE"),
		StrictHeightCheck:         strings.ToLower(os.Getenv("STRICT_HEIGHT_CHECK")) != "false",
		DriftTolerance:            getEnvAsInt64("DRIFT_TOLERANCE", 5),
		WatchedTokenAddresses:     parseWatchedTokens(),
		TokenFilterMode:           getEnv("TOKEN_FILTER_MODE", "whitelist"),
		Port:                      getEnv("PORT", "8080"),
		AppTitle:                  getEnv("APP_TITLE", "🚀 Web3 Indexer Dashboard"),
	}

	applyLocalAnvilFallback(cfg)
	logArchitecture(cfg)

	return cfg
}

func loadDotEnv() {
	_ = godotenv.Load()             // nolint:errcheck // optional loading
	_ = godotenv.Load("../.env")    // nolint:errcheck // optional loading
	_ = godotenv.Load("../../.env") // nolint:errcheck // optional loading
}

func isEnvTrue(key string) bool {
	return strings.ToLower(os.Getenv(key)) == "true"
}

func resolveSimulatorFlag(demoMode bool, chainID int64) bool {
	if val := os.Getenv("ENABLE_SIMULATOR"); val != "" {
		return strings.EqualFold(val, "true")
	}
	return demoMode || chainID == 31337
}

func applySecurityLock(networkMode string, chainID int64, current bool) bool {
	if networkMode != "anvil" && chainID != 31337 && current {
		log.Printf("🔒 SECURITY_LOCK: NetworkMode=%s detected. Forcing ENABLE_SIMULATOR=false", networkMode)
		return false
	}
	return current
}

func parseRPCUrls(_ bool) []string {
	urlsStr := getEnv("RPC_URLS", "https://eth.llamarpc.com")
	urls := strings.Split(urlsStr, ",")
	for i, u := range urls {
		urls[i] = strings.TrimSpace(u)
	}
	return urls
}

func detectTestnet(urls []string) bool {
	for _, u := range urls {
		l := strings.ToLower(u)
		if strings.Contains(l, "sepolia") || strings.Contains(l, "holesky") || strings.Contains(l, "goerli") {
			return true
		}
	}
	return false
}

func parseStartBlock() (int64, string) {
	val := getEnv("START_BLOCK", "0")
	if val == "latest" {
		return -1, val
	}
	return getEnvAsInt64("START_BLOCK", 0), val
}

func parseWatchedTokens() []string {
	val := getEnv("WATCHED_TOKEN_ADDRESSES", "")
	if val == "" {
		return nil
	}
	tokens := strings.Split(val, ",")
	for i, t := range tokens {
		tokens[i] = strings.TrimSpace(t)
	}
	return tokens
}

func applyLocalAnvilFallback(cfg *Config) {
	if os.Getenv("RPC_URLS") == "" && cfg.DemoMode {
		cfg.RPCURLs = []string{"http://127.0.0.1:8545"}
		cfg.ChainID = 31337
		cfg.RPCRateLimit = 200
		log.Printf("🔒 SECURITY_LOCK: NO RPC_URLS FOUND, FALLING BACK TO LOCAL ANVIL (targets=%v)", cfg.RPCURLs)
	}
}

func logArchitecture(cfg *Config) {
	networkName := "Mainnet"
	if cfg.ChainID == 11155111 {
		networkName = "Sepolia"
	} else if cfg.ChainID == 31337 {
		networkName = "Anvil"
	}
	log.Printf("🚀 Architecture Loaded: mode=%v network=%s rps=%d targets=%d",
		cfg.DemoMode, networkName, cfg.RPCRateLimit, len(cfg.RPCURLs))
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
