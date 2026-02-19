package engine

import (
	"strings"
	"testing"
	"time"
)

// TestIntegration_Quota_Exhaustion_AI_Friendly 验证配额耗尽时的熔断
func TestIntegration_Quota_Exhaustion_AI_Friendly(t *testing.T) {
	limit := int64(1000)
	guard := NewQuotaGuard(limit)

	// 1. 模拟额度已满
	guard.SetCurrentUsed(limit + 1)

	// 2. 尝试发起请求
	allowed := guard.AllowRequest()

	// 3. 验证是否被拦截
	if allowed {
		t.Fatalf("AI_FIX_REQUIRED: System failed to halt on quota exhaustion. Current: %d, Limit: %d",
			guard.CurrentUsed, limit)
	}

	// 4. 验证模式
	if guard.GetMode() != QuotaModeExhausted {
		t.Errorf("AI_FIX_REQUIRED: Expected EXHAUSTED mode, got %s", guard.GetMode())
	}

	t.Logf("✅ SUCCESS: Quota exhaustion halt works correctly")
}

// TestQuotaGuard_StrictMode 验证严格模式的延迟
func TestQuotaGuard_StrictMode(t *testing.T) {
	limit := int64(1000)
	guard := NewQuotaGuard(limit)

	// 设置到 95%（触发严格模式）
	guard.SetCurrentUsed(int64(float64(limit) * 0.95))

	start := time.Now()
	allowed := guard.AllowRequest()
	elapsed := time.Since(start)

	if !allowed {
		t.Fatal("AI_FIX_REQUIRED: STRICT mode should still allow requests")
	}

	if elapsed < 2*time.Second {
		t.Errorf("AI_FIX_REQUIRED: STRICT mode should enforce 2s delay, got %v", elapsed)
	}

	if guard.GetMode() != QuotaModeStrict {
		t.Errorf("AI_FIX_REQUIRED: Expected STRICT mode, got %s", guard.GetMode())
	}

	t.Logf("✅ SUCCESS: Strict mode delay enforced (%v)", elapsed)
}

// TestQuotaGuard_ThrottlingMode 验证节流模式
func TestQuotaGuard_ThrottlingMode(t *testing.T) {
	limit := int64(1000)
	guard := NewQuotaGuard(limit)

	// 设置到 87%（触发节流模式）
	guard.SetCurrentUsed(int64(float64(limit) * 0.87))

	allowed := guard.AllowRequest()

	if !allowed {
		t.Fatal("AI_FIX_REQUIRED: THROTTLING mode should allow requests")
	}

	if guard.GetMode() != QuotaModeThrottling {
		t.Errorf("AI_FIX_REQUIRED: Expected THROTTLING mode, got %s", guard.GetMode())
	}

	t.Logf("✅ SUCCESS: Throttling mode activated")
}

// TestQuotaGuard_AutoReset 验证自动重置
func TestQuotaGuard_AutoReset(t *testing.T) {
	guard := NewQuotaGuard(1000)
	guard.LastResetTime = time.Now().Add(-25 * time.Hour) // 模拟跨天

	guard.AllowRequest()

	if guard.CurrentUsed != 0 {
		t.Errorf("AI_FIX_REQUIRED: Auto-reset failed, used=%d", guard.CurrentUsed)
	}

	t.Logf("✅ SUCCESS: Auto reset works")
}
