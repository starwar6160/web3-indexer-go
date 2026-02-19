package engine

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// QuotaGuard RPC 预算守护者
// 设计理念：在博彩系统中，配额耗尽等同于服务熔断
type QuotaGuard struct {
	mu sync.RWMutex

	// 配额配置
	DailyLimit      int64   // 每日限额（如 Infura 免费 100K）
	CurrentUsed     int64   // 已使用量
	StrictThreshold float64 // 严格模式阈值（如 0.9 = 90%）
	LastResetTime   time.Time

	// 状态
	mode QuotaMode

	// 统计
	requestsDenied atomic.Int64
}

// QuotaMode 配额模式
type QuotaMode int

const (
	QuotaModeNormal     QuotaMode = iota // 正常模式
	QuotaModeThrottling                  // 节流模式（> 85%）
	QuotaModeStrict                      // 严格模式（> 90%）
	QuotaModeExhausted                   // 耗尽模式（100%）
)

// NewQuotaGuard 创建配额守护者
func NewQuotaGuard(dailyLimit int64) *QuotaGuard {
	return &QuotaGuard{
		DailyLimit:      dailyLimit,
		CurrentUsed:     0,
		StrictThreshold: 0.90, // 90% 触发严格模式
		LastResetTime:   time.Now(),
		mode:            QuotaModeNormal,
	}
}

// AllowRequest 是否允许发起请求（核心拦截器）
func (g *QuotaGuard) AllowRequest() bool {
	g.mu.RLock()
	usageRatio := float64(g.CurrentUsed) / float64(g.DailyLimit)
	g.mu.RUnlock()

	// 1. 硬熔断：配额耗尽
	if usageRatio >= 1.0 {
		g.requestsDenied.Add(1)
		return false
	}

	// 2. 严格模式：增加延迟（主动降速）
	if usageRatio > g.StrictThreshold {
		g.updateMode(QuotaModeStrict)
		// 极度干旱模式：强制休眠
		time.Sleep(2 * time.Second)
		return true
	}

	// 3. 节流模式（85%-90%）
	if usageRatio > 0.85 {
		g.updateMode(QuotaModeThrottling)
		return true
	}

	// 4. 正常模式
	g.updateMode(QuotaModeNormal)
	return true
}

// RecordRequest 记录一次请求
func (g *QuotaGuard) RecordRequest() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.CurrentUsed++

	// 自动重置（跨午夜）
	if time.Since(g.LastResetTime) > 24*time.Hour {
		g.Reset()
	}
}

// GetUsageRatio 获取使用率（0-1）
func (g *QuotaGuard) GetUsageRatio() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return float64(g.CurrentUsed) / float64(g.DailyLimit)
}

// GetMode 获取当前模式
func (g *QuotaGuard) GetMode() QuotaMode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.mode
}

// SetCurrentUsed 手动设置已使用量（从外部 API 同步）
func (g *QuotaGuard) SetCurrentUsed(used int64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.CurrentUsed = used

	// 自动检测模式
	usageRatio := float64(used) / float64(g.DailyLimit)
	if usageRatio >= 1.0 {
		g.mode = QuotaModeExhausted
		slog.Warn("🚨 QUOTA_EXHAUSTED: Hard halt triggered", "used", used, "limit", g.DailyLimit)
	} else if usageRatio > g.StrictThreshold {
		g.mode = QuotaModeStrict
		slog.Warn("⚠️ QUOTA_STRICT: Throttling activated", "used", used, "ratio", usageRatio)
	}
}

// Reset 重置配额（每日重置）
func (g *QuotaGuard) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	oldUsed := g.CurrentUsed
	g.CurrentUsed = 0
	g.LastResetTime = time.Now()
	g.mode = QuotaModeNormal

	slog.Info("🔄 QUOTA_RESET: Daily cycle completed", "old_used", oldUsed)
}

// GetMetrics 获取配额指标
func (g *QuotaGuard) GetMetrics() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return map[string]interface{}{
		"daily_limit":     g.DailyLimit,
		"current_used":    g.CurrentUsed,
		"remaining":       g.DailyLimit - g.CurrentUsed,
		"usage_ratio":     g.GetUsageRatio(),
		"mode":            g.mode.String(),
		"requests_denied": g.requestsDenied.Load(),
	}
}

// updateMode 更新模式
func (g *QuotaGuard) updateMode(mode QuotaMode) {
	if g.mode != mode {
		oldMode := g.mode
		g.mode = mode
		slog.Info("🔧 QUOTA_MODE_CHANGED",
			"old", oldMode.String(),
			"new", mode.String(),
			"usage_ratio", g.GetUsageRatio())
	}
}

// String 实现 Stringer 接口
func (m QuotaMode) String() string {
	switch m {
	case QuotaModeNormal:
		return "NORMAL"
	case QuotaModeThrottling:
		return "THROTTLING"
	case QuotaModeStrict:
		return "STRICT"
	case QuotaModeExhausted:
		return "EXHAUSTED"
	default:
		return "UNKNOWN"
	}
}
