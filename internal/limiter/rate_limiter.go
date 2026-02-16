package limiter

import (
	"context"
	"log/slog"

	"golang.org/x/time/rate"
)

// 🛡️ 工业级硬编码保护
const (
	MaxSafetyRPS     = 3 // 绝对安全上限：每秒 3 次请求
	DefaultBurstSize = 1 // 允许 1 个并发突发
)

// RateLimiter 速率限制器，带有工业级安全保护
type RateLimiter struct {
	limiter *rate.Limiter
	maxRPS  int // 记录配置的 RPS（用于审计）
}

// NewRateLimiter 创建一个新的限流器
// 优先使用硬编码安全值，如果环境变量超过上限则强制降级
func NewRateLimiter(envRPS int) *RateLimiter {
	// 1. 默认采用硬编码的最安全值
	rps := MaxSafetyRPS

	// 2. 核心安全审计：如果外部传入的值超过了硬编码上限，强制降级
	if envRPS > 0 && envRPS <= MaxSafetyRPS {
		rps = envRPS
		slog.Info("✅ Rate limiter configured",
			"rps", rps,
			"mode", "safe")
	} else if envRPS > MaxSafetyRPS {
		slog.Warn("⚠️  Unsafe RPS config detected, forcing safe threshold",
			"requested_rps", envRPS,
			"forced_rps", MaxSafetyRPS,
			"reason", "commercial_quota_protection")
		rps = MaxSafetyRPS
	} else {
		slog.Info("✅ Rate limiter using default safe value",
			"rps", rps,
			"mode", "default")
	}

	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(rps), DefaultBurstSize),
		maxRPS:  rps,
	}
}

// Wait 阻塞直到获取令牌（或上下文取消）
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.limiter.Wait(ctx)
}

// MaxRPS 返回当前配置的最大 RPS（用于监控）
func (rl *RateLimiter) MaxRPS() int {
	return rl.maxRPS
}

// GetRPSEstimate 返回每秒实际消耗的 RPS 估算值
func (rl *RateLimiter) GetRPSEstimate() float64 {
	limit := float64(rl.limiter.Limit())
	// 保守估算：限制值的 80% 作为实际 RPS
	return limit * 0.8
}

// Limiter 返回内部 rate.Limiter 实例（用于兼容现有代码）
func (rl *RateLimiter) Limiter() *rate.Limiter {
	return rl.limiter
}
