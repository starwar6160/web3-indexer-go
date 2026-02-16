package monitor

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	MaxDailyQuota      = 300000 // 商业节点每日免费额度上限（CU）
	AlertThreshold     = 0.80   // 80% 预警阈值
	CriticalThreshold  = 0.90   // 90% 临界阈值
)

// QuotaMonitor RPC 额度监控器
type QuotaMonitor struct {
	dailyCalls  uint64      // 当天 RPC 调用次数
	resetTime   time.Time   // 下次重置时间（UTC 0 点）
	usageGauge  prometheus.Gauge
	statusGauge prometheus.Gauge
}

// NewQuotaMonitor 创建新的额度监控器
func NewQuotaMonitor() *QuotaMonitor {
	qm := &QuotaMonitor{
		usageGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "rpc_quota_usage_percent",
			Help: "Percentage of daily RPC quota used (0-100)",
		}),
		statusGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "rpc_quota_status",
			Help: "RPC quota status: 0=Safe, 1=Warning, 2=Critical",
		}),
	}
	qm.resetTime = qm.calculateNextReset()
	go qm.startResetTimer()

	slog.Info("🛡️ Quota monitor initialized",
		"max_daily_quota", MaxDailyQuota,
		"alert_threshold", AlertThreshold*100,
		"critical_threshold", CriticalThreshold*100)

	return qm
}

// Inc 每次调用 RPC 前调用此方法
func (m *QuotaMonitor) Inc() {
	current := atomic.AddUint64(&m.dailyCalls, 1)
	usagePercent := float64(current) / float64(MaxDailyQuota)

	// 更新 Prometheus 指标
	m.usageGauge.Set(usagePercent * 100)

	// 更新状态指标
	status := 0.0 // Safe
	if usagePercent >= CriticalThreshold {
		status = 2.0 // Critical
	} else if usagePercent >= AlertThreshold {
		status = 1.0 // Warning
	}
	m.statusGauge.Set(status)

	// 阈值检查（每 100 次检查一次，避免日志刷屏）
	if current%100 == 0 {
		if usagePercent >= CriticalThreshold {
			slog.Error("🛑 CRITICAL: RPC quota nearly exhausted!",
				"usage_percent", usagePercent*100,
				"calls", current,
				"max_quota", MaxDailyQuota,
				"action", "consider_switching_to_idle_mode")
		} else if usagePercent >= AlertThreshold {
			slog.Warn("⚠️  QUOTA WARNING: RPC usage exceeds threshold",
				"usage_percent", usagePercent*100,
				"calls", current,
				"max_quota", MaxDailyQuota,
				"remaining", MaxDailyQuota-current)
		}
	}
}

// GetUsagePercent 返回当前使用率（0-100）
func (m *QuotaMonitor) GetUsagePercent() float64 {
	current := atomic.LoadUint64(&m.dailyCalls)
	return float64(current) / float64(MaxDailyQuota) * 100
}

// calculateNextReset 计算下一个 UTC 0 点
func (m *QuotaMonitor) calculateNextReset() time.Time {
	now := time.Now().UTC()
	nextReset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return nextReset
}

// startResetTimer 启动每日重置定时器
func (m *QuotaMonitor) startResetTimer() {
	for {
		now := time.Now().UTC()
		duration := m.resetTime.Sub(now)

		if duration > 0 {
			slog.Info("⏰ Quota monitor reset timer scheduled",
				"next_reset", m.resetTime.Format(time.RFC3339),
				"duration_hours", duration.Hours())
			time.Sleep(duration)
		}

		// 执行重置
		m.ResetDaily()
		m.resetTime = m.calculateNextReset()
	}
}

// ResetDaily 重置每日计数器（由定时任务调用）
func (m *QuotaMonitor) ResetDaily() {
	atomic.StoreUint64(&m.dailyCalls, 0)
	m.usageGauge.Set(0)
	m.statusGauge.Set(0)
	slog.Info("📅 Daily RPC quota counter reset",
		"time_utc", time.Now().UTC().Format(time.RFC3339))
}
