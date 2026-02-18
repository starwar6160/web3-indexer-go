package engine

import (
	"log/slog"
)

// PerformanceProfile 环境感知的性能配置文件
// 用于横滨实验室（128G RAM）的极限性能优化
type PerformanceProfile struct {
	Name                 string
	AllowSleep           bool     // 是否允许休眠
	EnforceQuota         bool     // 是否强制配额限制
	BatchSize            int      // 批处理大小
	ChannelBufferSize    int      // Channel 缓冲区大小
	MaxOpenConns         int      // 数据库最大连接数
	FetchConcurrency     int      // 抓取并发数
	TPSLimit             float64  // TPS 限制
	MetadataCacheSize    int      // 元数据缓存大小
	EnableAggressiveBatch bool     // 启用激进批处理
}

// GetPerformanceProfile 根据 RPC URL 和 ChainID 自动获取最优性能配置
func GetPerformanceProfile(rpcURLs []string, chainID int64) *PerformanceProfile {
	// 优先级 1: ChainID 显式检测
	if chainID == 31337 {
		return YokohamaLabProfile()
	}

	// 优先级 2: RPC URL 特征检测
	for _, url := range rpcURLs {
		if IsLocalAnvil(url) {
			slog.Info("🔥 Anvil environment detected", "url", url, "profile", "Yokohama Lab")
			return YokohamaLabProfile()
		}
	}

	// 默认: 生产环境保守配置
	return ProductionProfile()
}

// YokohamaLabProfile 横滨实验室极限性能配置
// 目标：在 128G RAM 环境下，不限流，榨干 Anvil 性能
func YokohamaLabProfile() *PerformanceProfile {
	return &PerformanceProfile{
		Name:                 "Yokohama Lab (Unlimited)",
		AllowSleep:           false,     // 永远不睡
		EnforceQuota:         false,     // 无视配额
		BatchSize:            100,       // 激进批处理（vs 默认 2-3）
		ChannelBufferSize:    10000,     // 超大缓冲区（vs 默认 100）
		MaxOpenConns:         100,       // 无限火力数据库连接
		FetchConcurrency:     16,        // 高并发抓取
		TPSLimit:             100000.0,  // 实际无限（100k TPS）
		MetadataCacheSize:    100000,    // 超大元数据缓存
		EnableAggressiveBatch: true,     // 启用滑动时间窗口批处理
	}
}

// ProductionProfile 生产环境保守配置
// 目标：稳定性和成本控制
func ProductionProfile() *PerformanceProfile {
	return &PerformanceProfile{
		Name:                 "Production (Conservative)",
		AllowSleep:           true,      // 允许 Eco-Mode
		EnforceQuota:         true,      // 强制配额限制
		BatchSize:            20,        // 保守批次
		ChannelBufferSize:    100,       // 默认缓冲区
		MaxOpenConns:         25,        // 保守连接数
		FetchConcurrency:     10,        // 默认并发
		TPSLimit:             20.0,      // 保守 TPS
		MetadataCacheSize:    10000,     // 默认缓存
		EnableAggressiveBatch: false,    // 禁用激进批处理
	}
}

// ApplyToConfig 应用性能配置到全局配置（用于环境覆盖）
func (p *PerformanceProfile) ApplyToConfig(cfg interface{}) {
	slog.Info("🚀 Applying Performance Profile",
		"name", p.Name,
		"batch_size", p.BatchSize,
		"channel_buffer", p.ChannelBufferSize,
		"tps_limit", p.TPSLimit,
		"max_conns", p.MaxOpenConns,
	)
}
