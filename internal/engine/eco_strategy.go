package engine

import (
	"log/slog"
	"sync"
	"time"
)

// HeatStrategy 链热度感应策略
// 设计理念：像猎人一样潜伏，平时低频采样节省 Quota，检测到热度时全速爆发
type HeatStrategy struct {
	mu sync.RWMutex

	// 滑动窗口：存储过去 60 秒的 TPS
	tpsWindow  []float64
	windowSize int

	// 阈值配置
	wakeThreshold    float64 // 唤醒阈值：平均 TPS > 此值时唤醒
	racingThreshold  float64 // 竞速阈值：进入全速模式
	coolingThreshold float64 // 冷却阈值：低于此值进入节能模式

	// 状态
	isExhausted bool // Quota 耗尽强制休眠
	lastHeat    float64
	lastUpdate  time.Time
}

// NewHeatStrategy 创建热度策略
func NewHeatStrategy() *HeatStrategy {
	return &HeatStrategy{
		tpsWindow:        make([]float64, 0, 60),
		windowSize:       60,
		wakeThreshold:    2.0, // 平均 TPS > 2.0 时唤醒
		racingThreshold:  5.0, // TPS > 5.0 时全速
		coolingThreshold: 0.5, // TPS < 0.5 时冷却
		lastUpdate:       time.Now(),
	}
}

// RecordTPS 记录当前 TPS
func (s *HeatStrategy) RecordTPS(tps float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tpsWindow = append(s.tpsWindow, tps)
	if len(s.tpsWindow) > s.windowSize {
		s.tpsWindow = s.tpsWindow[1:]
	}
	s.lastHeat = tps
	s.lastUpdate = time.Now()
}

// ShouldWakeUp 是否应该唤醒 Eco-Mode
// 核心逻辑：不仅仅看有没有人看网页，还要看链上有没有"大鱼"
func (s *HeatStrategy) ShouldWakeUp(currentTPS float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 强制休眠状态（Quota 耗尽）
	if s.isExhausted {
		return false
	}

	// 记录当前采样值，保证推荐频率能立即感知突发热度
	avg := s.calculateAverage()
	s.tpsWindow = append(s.tpsWindow, currentTPS)
	if len(s.tpsWindow) > s.windowSize {
		s.tpsWindow = s.tpsWindow[1:]
	}
	s.lastHeat = currentTPS
	s.lastUpdate = time.Now()

	// 2. 当前 TPS 远高于历史平均值（检测突发热度）
	if currentTPS > avg*2.0 {
		slog.Info("🔥 HEAT_SPIKE: Detected sudden TPS surge",
			"current_tps", currentTPS,
			"avg_tps", avg,
			"ratio", currentTPS/avg)
		return true
	}

	// 3. 当前 TPS 超过唤醒阈值
	if currentTPS > s.wakeThreshold {
		return true
	}

	return false
}

// GetRecommendedPace 根据热度推荐采样频率
func (s *HeatStrategy) GetRecommendedPace() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastHeat >= s.racingThreshold {
		return 200 * time.Millisecond
	}

	avg := s.calculateAverage()

	switch {
	case avg >= s.racingThreshold:
		// 全速冲刺：检测到交易高峰（NFT Drop 等）
		return 200 * time.Millisecond
	case avg >= s.wakeThreshold:
		// 正常同步
		return 1 * time.Second
	case avg >= s.coolingThreshold:
		// 低频采样
		return 5 * time.Second
	default:
		// 深度休眠
		return 30 * time.Second
	}
}

// GetCurrentHeat 获取当前热度（0-100）
func (s *HeatStrategy) GetCurrentHeat() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.tpsWindow) == 0 {
		return 0.0
	}

	// 归一化到 0-100
	avg := s.calculateAverage()
	heat := (avg / s.racingThreshold) * 100.0
	if heat > 100 {
		heat = 100.0
	}
	return heat
}

// calculateAverage 计算滑动窗口平均值
func (s *HeatStrategy) calculateAverage() float64 {
	if len(s.tpsWindow) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, tps := range s.tpsWindow {
		sum += tps
	}
	return sum / float64(len(s.tpsWindow))
}

// ForceExhaustion 强制进入耗尽状态
func (s *HeatStrategy) ForceExhaustion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isExhausted = true
	slog.Warn("🚨 QUOTA_EXHAUSTED: Forced into deep sleep")
}

// ResetExhaustion 重置耗尽状态
func (s *HeatStrategy) ResetExhaustion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isExhausted = false
	slog.Info("✅ QUOTA_RECOVERED: Exiting forced sleep")
}
