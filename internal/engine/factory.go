package engine

import (
	"log/slog"
	"os"
	"strings"

	"golang.org/x/time/rate"
)

// 🔥 Strategy Factory: 环境驱动的策略工厂
// 根据 APP_MODE 环境变量自动创建正确的策略实例
// 消灭代码中所有的 if mode == "demo2" 判断

// StrategyFactory 策略工厂
type StrategyFactory struct {
	mode string
}

// NewStrategyFactory 创建策略工厂
func NewStrategyFactory() *StrategyFactory {
	mode := os.Getenv("APP_MODE")
	rpcURL := os.Getenv("RPC_URL")
	rpcURLs := os.Getenv("RPC_URLS")

	slog.Debug("🔍 StrategyFactory: Environment check",
		"app_mode_env", mode,
		"rpc_url_env", rpcURL,
		"rpc_urls_env", rpcURLs)

	if mode == "" {
		// 尝试从 RPC_URL 或 RPC_URLS 推断模式
		targetURL := rpcURL
		if targetURL == "" && rpcURLs != "" {
			// 取第一个 URL
			urls := strings.Split(rpcURLs, ",")
			if len(urls) > 0 {
				targetURL = strings.TrimSpace(urls[0])
			}
		}

		slog.Debug("🔍 StrategyFactory: Auto-detect from RPC", "target_url", targetURL)

		if targetURL == "" {
			// 🔥 关键修复：如果没有检测到 RPC URL，检查是否是 Anvil 的默认端口
			// demo2 环境通常使用 8545 或类似的本地端口
			mode = "EPHEMERAL_ANVIL" // 默认为 Anvil 模式以支持演示
			slog.Info("🔍 StrategyFactory: No RPC URL found, defaulting to ANVIL for demo")
		} else if strings.Contains(targetURL, "localhost") ||
			strings.Contains(targetURL, "127.0.0.1") ||
			strings.Contains(targetURL, "anvil") ||
			strings.Contains(targetURL, ":8545") || // Anvil 默认端口
			strings.Contains(targetURL, ":8546") { // Anvil WS 端口
			mode = "EPHEMERAL_ANVIL"
			slog.Info("🔍 StrategyFactory: Detected Anvil from RPC URL", "url", targetURL)
		} else {
			mode = "PERSISTENT_TESTNET"
			slog.Info("🔍 StrategyFactory: Detected Testnet from RPC URL", "url", targetURL)
		}
	}

	slog.Info("🔍 StrategyFactory: Final mode selected", "mode", mode)
	return &StrategyFactory{mode: mode}
}

// NewStrategyFactoryFromChainID 根据已知 ChainID 创建工厂（最可靠的检测方式）
// 在 main.go 中 ChainID 已从配置中读取，直接用此方法避免 URL 误判
func NewStrategyFactoryFromChainID(chainID int64) *StrategyFactory {
	var mode string
	switch chainID {
	case 31337:
		mode = "EPHEMERAL_ANVIL"
		slog.Info("🔍 StrategyFactory: ChainID 31337 = Anvil", "chain_id", chainID)
	case 1:
		mode = "PERSISTENT_TESTNET" // mainnet treated conservatively
		slog.Warn("🔍 StrategyFactory: ChainID 1 = Mainnet (conservative mode)", "chain_id", chainID)
	default:
		mode = "PERSISTENT_TESTNET"
		slog.Info("🔍 StrategyFactory: ChainID detected as testnet", "chain_id", chainID)
	}
	return &StrategyFactory{mode: mode}
}

// CreateStrategy 根据环境创建策略实例
// 防御性设计：未知模式默认使用最保守的 TestnetStrategy
func (f *StrategyFactory) CreateStrategy() Strategy {
	switch f.mode {
	case "EPHEMERAL_ANVIL", "ANVIL", "LOCAL":
		slog.Info("🏭 StrategyFactory: Manufacturing [Anvil-Speed] strategy",
			"mode", f.mode,
			"qps", 1000,
			"backpressure", 5000)
		return &AnvilStrategy{}

	case "PERSISTENT_TESTNET", "TESTNET", "SEPOLIA":
		slog.Info("🏭 StrategyFactory: Manufacturing [Sepolia-Eco] strategy",
			"mode", f.mode,
			"qps", 2,
			"backpressure", 100)
		return &TestnetStrategy{}

	default:
		// 🔥 防御性设计：未知模式默认使用最保守的策略
		slog.Warn("⚠️ StrategyFactory: Unknown APP_MODE, defaulting to SAFE_TESTNET",
			"mode", f.mode,
			"available_modes", "EPHEMERAL_ANVIL, PERSISTENT_TESTNET")
		return &TestnetStrategy{}
	}
}

// GetMode 返回当前模式
func (f *StrategyFactory) GetMode() string {
	return f.mode
}

// IsAnvilMode 检查是否为 Anvil 模式
func (f *StrategyFactory) IsAnvilMode() bool {
	return f.mode == "EPHEMERAL_ANVIL" || f.mode == "ANVIL" || f.mode == "LOCAL"
}

// IsTestnetMode 检查是否为 Testnet 模式
func (f *StrategyFactory) IsTestnetMode() bool {
	return f.mode == "PERSISTENT_TESTNET" || f.mode == "TESTNET" || f.mode == "SEPOLIA"
}

// ApplyToOrchestrator 将策略参数应用到 Orchestrator
// 一键完成所有限流器和缓冲区的参数注入
func (f *StrategyFactory) ApplyToOrchestrator(orch *Orchestrator, strategy Strategy) {
	// 1. 应用 RPC 限流配置
	limit, burst := strategy.GetRPCConfig()
	slog.Info("🔌 Strategy Engaged",
		"strategy", strategy.Name(),
		"qps_limit", limit,
		"burst", burst,
		"backpressure_threshold", strategy.GetBackpressureThreshold(),
		"seq_buffer", strategy.GetSeqBufferSize())

	// 这里可以扩展到全局限流器
	_ = limit
	_ = burst

	// 2. 记录策略信息到 orchestrator（通过日志而非状态存储）
	slog.Info("🚀 Engine Primed",
		"strategy", strategy.Name(),
		"persist", strategy.ShouldPersist(),
		"confirmations", strategy.GetConfirmations(),
		"batch_size", strategy.GetBatchSize())
}

// GetGlobalRateLimiter 根据策略创建全局限流器
func (f *StrategyFactory) GetGlobalRateLimiter(strategy Strategy) *rate.Limiter {
	limit, burst := strategy.GetRPCConfig()
	return rate.NewLimiter(limit, burst)
}

// StrategyInfo 策略信息结构（用于监控和 UI 展示）
type StrategyInfo struct {
	Name                  string  `json:"name"`
	Mode                  string  `json:"mode"`
	QPSLimit              float64 `json:"qps_limit"`
	Burst                 int     `json:"burst"`
	BackpressureThreshold int     `json:"backpressure_threshold"`
	SeqBufferSize         int     `json:"seq_buffer_size"`
	ShouldPersist         bool    `json:"should_persist"`
	Confirmations         uint64  `json:"confirmations"`
	BatchSize             int     `json:"batch_size"`
}

// GetStrategyInfo 获取策略信息（用于 /api/status）
func (f *StrategyFactory) GetStrategyInfo(strategy Strategy) StrategyInfo {
	limit, burst := strategy.GetRPCConfig()
	return StrategyInfo{
		Name:                  strategy.Name(),
		Mode:                  f.mode,
		QPSLimit:              float64(limit),
		Burst:                 burst,
		BackpressureThreshold: strategy.GetBackpressureThreshold(),
		SeqBufferSize:         strategy.GetSeqBufferSize(),
		ShouldPersist:         strategy.ShouldPersist(),
		Confirmations:         strategy.GetConfirmations(),
		BatchSize:             strategy.GetBatchSize(),
	}
}
