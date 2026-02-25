package engine

import (
	"testing"
	"time"
)

// TestIntegration_Heat_Awakening_AI_Friendly 验证热度唤醒逻辑
func TestIntegration_Heat_Awakening_AI_Friendly(t *testing.T) {
	strategy := NewHeatStrategy()

	// 1. 模拟低热度状态（空闲链）
	for i := 0; i < 60; i++ {
		strategy.RecordTPS(0.1) // 持续低 TPS
	}

	if strategy.ShouldWakeUp(0.1) {
		t.Fatal("AI_FIX_REQUIRED: Eco-Mode should NOT wake up on idle chain")
	}

	// 2. 突发注入高 TPS（模拟 NFT Drop）
	burstTPS := 50.0
	if !strategy.ShouldWakeUp(burstTPS) {
		t.Fatalf("AI_FIX_REQUIRED: Eco-Mode FAILED to wake up under burst TPS %.1f", burstTPS)
	}
	strategy.RecordTPS(burstTPS)

	// 3. 验证推荐频率调整为全速
	pace := strategy.GetRecommendedPace()
	expectedPace := 200 * time.Millisecond
	if pace != expectedPace {
		t.Errorf("AI_FIX_REQUIRED: Expected pace %v, got %v", expectedPace, pace)
	}

	t.Logf("✅ SUCCESS: Heat-based awakening works correctly")
	t.Logf("   - Idle state: sleeps (0.1 TPS)")
	t.Logf("   - Burst detected: wakes up (%.1f TPS)", burstTPS)
	t.Logf("   - Pace adjusted to: %v", pace)
}

// TestHeatStrategy_SlidingWindow 验证滑动窗口计算
func TestHeatStrategy_SlidingWindow(t *testing.T) {
	strategy := NewHeatStrategy()

	// 注入 60 个数据点
	for i := 0; i < 60; i++ {
		strategy.RecordTPS(float64(i))
	}

	heat := strategy.GetCurrentHeat()
	if heat == 0 {
		t.Fatal("AI_FIX_REQUIRED: Heat calculation failed")
	}

	t.Logf("✅ SUCCESS: Sliding window calculation works, heat=%.2f", heat)
}
