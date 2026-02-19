package engine

import (
	"testing"
	"time"
)

// TestIntegration_AddressLabeling_Latency_AI_Friendly 验证地址标签查询速度
func TestIntegration_AddressLabeling_Latency_AI_Friendly(t *testing.T) {
	am := NewAddressManager()

	start := time.Now()
	// 模拟连续查询 10,000 次地址
	for i := 0; i < 10000; i++ {
		_ = am.GetLabel("0x71C7656EC7ab88b098defB751B7401B5f6d8976F")
	}
	duration := time.Since(start)

	// 🚀 断言：本地 Map 查询应该是纳秒级的
	if duration.Milliseconds() > 10 {
		t.Errorf("AI_FIX_REQUIRED: Address lookup is too slow (%v). Expected < 10ms, got %v",
			duration, duration.Milliseconds())
	}

	t.Logf("✅ SUCCESS: 10,000 lookups in %v (%.2f μs/op)",
		duration, float64(duration.Microseconds())/10000.0)
}

// TestAddressManager_IdentifyCoreAddresses 验证核心地址识别
func TestAddressManager_IdentifyCoreAddresses(t *testing.T) {
	am := NewAddressManager()

	tests := []struct {
		address      string
		expectedName string
		expectedIcon string
	}{
		{
			address:      "0x0000000000000000000000000000000000000000",
			expectedName: "Null Address",
			expectedIcon: "🔥",
		},
		{
			address:      "0x71C7656EC7ab88b098defB751B7401B5f6d8976F",
			expectedName: "Binance: Cold Wallet",
			expectedIcon: "🏦",
		},
		{
			address:      "0xdAC17F958D2ee523a2206206994597C13D831ec7",
			expectedName: "Tether: USDT",
			expectedIcon: "💵",
		},
		{
			address:      "0x1f9840a85d5af5bf1d1762f925bdaddc4201f984",
			expectedName: "Uniswap V3: Factory",
			expectedIcon: "🦄",
		},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			label := am.Identify(tt.address)

			if label == nil {
				t.Fatalf("AI_FIX_REQUIRED: Failed to identify address %s", tt.address)
			}

			if label.Name != tt.expectedName {
				t.Errorf("AI_FIX_REQUIRED: Name mismatch. Expected %s, got %s",
					tt.expectedName, label.Name)
			}

			if label.Icon != tt.expectedIcon {
				t.Errorf("AI_FIX_REQUIRED: Icon mismatch. Expected %s, got %s",
					tt.expectedIcon, label.Icon)
			}

			t.Logf("✅ SUCCESS: %s correctly identified as %s %s",
				tt.address[:10]+"...", label.Icon, label.Name)
		})
	}
}

// TestAddressManager_GetLabelWithIcon 验证带图标的标签
func TestAddressManager_GetLabelWithIcon(t *testing.T) {
	am := NewAddressManager()

	result := am.GetLabelWithIcon("0x71C7656EC7ab88b098defB751B7401B5f6d8976F")
	expected := "🏦 Binance: Cold Wallet"

	if result != expected {
		t.Errorf("AI_FIX_REQUIRED: Expected %s, got %s", expected, result)
	}

	t.Logf("✅ SUCCESS: Icon label correct: %s", result)
}

// TestAddressManager_UnknownAddress 验证未知地址的降级处理
func TestAddressManager_UnknownAddress(t *testing.T) {
	am := NewAddressManager()

	unknownAddr := "0x1234567890123456789012345678901234567890"
	result := am.GetLabel(unknownAddr)

	// 应该返回截断的地址
	expected := "0x1234...7890"
	if result != expected {
		t.Errorf("AI_FIX_REQUIRED: Unknown address should return truncated format. Expected %s, got %s",
			expected, result)
	}

	t.Logf("✅ SUCCESS: Unknown address correctly formatted as %s", result)
}

// TestAddressManager_GetStats 验证统计信息
func TestAddressManager_GetStats(t *testing.T) {
	am := NewAddressManager()

	// 触发一些查询
	am.GetLabel("0x71C7656EC7ab88b098defB751B7401B5f6d8976F") // hit
	am.GetLabel("0x1234567890123456789012345678901234567890") // miss

	stats := am.GetStats()

	totalLabels, ok := stats["total_labels"].(int)
	if !ok || totalLabels == 0 {
		t.Error("AI_FIX_REQUIRED: Total labels should be > 0")
	}

	hits, ok := stats["hits"].(int64)
	if !ok || hits == 0 {
		t.Error("AI_FIX_REQUIRED: Should have at least 1 hit")
	}

	misses, ok := stats["misses"].(int64)
	if !ok || misses == 0 {
		t.Error("AI_FIX_REQUIRED: Should have at least 1 miss")
	}

	t.Logf("✅ SUCCESS: Stats correct - Labels: %d, Hits: %d, Misses: %d, Hit Rate: %.1f%%",
		totalLabels, hits, misses, stats["hit_rate"])
}
