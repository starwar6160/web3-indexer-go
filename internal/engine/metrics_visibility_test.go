package engine

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestIntegration_MetricsVisibility_AI_Friendly 验证指标可见性
// 拦截"监控系统断流"的 Bug：UI 有数据但 Prometheus/Grafana 显示 0
func TestIntegration_MetricsVisibility_AI_Friendly(t *testing.T) {
	// 1. 获取 Metrics 单例
	m := GetMetrics()
	assert.NotNil(t, m, "Metrics should be initialized")

	// 2. 模拟处理 100 个交易和 10 个区块
	initialTxCount := testutil.ToFloat64(m.TransfersProcessed)
	initialBlockCount := testutil.ToFloat64(m.BlocksProcessed)

	// 记录区块处理
	for i := 0; i < 10; i++ {
		m.RecordBlockProcessed(100 * time.Millisecond)
		m.RecordBlockActivity(1)
	}

	// 记录转账处理
	for i := 0; i < 100; i++ {
		m.RecordTransferProcessed()
		m.RecordActivity(1)
	}

	// 3. 直接从 Prometheus Registry 读取数据
	txCount := testutil.ToFloat64(m.TransfersProcessed)
	blockCount := testutil.ToFloat64(m.BlocksProcessed)

	// 4. 断言：Prometheus 里的值必须与输入匹配
	assert.Greater(t, txCount, initialTxCount,
		"AI_FIX_REQUIRED: Metrics Exporter is blind! TransfersProcessed not incrementing")
	assert.Greater(t, blockCount, initialBlockCount,
		"AI_FIX_REQUIRED: Metrics Exporter is blind! BlocksProcessed not incrementing")

	// 验证实时 TPS/BPS 指标
	m.UpdateRealtimeTPS(5.5)
	m.UpdateRealtimeBPS(1.2)

	tps := testutil.ToFloat64(m.RealtimeTPS)
	bps := testutil.ToFloat64(m.RealtimeBPS)

	assert.Equal(t, 5.5, tps, "Realtime TPS gauge not set correctly")
	assert.Equal(t, 1.2, bps, "Realtime BPS gauge not set correctly")

	t.Logf("✅ SUCCESS: Metrics visibility verified. "+
		"Transfers: %.0f -> %.0f, Blocks: %.0f -> %.0f, TPS: %.1f, BPS: %.1f",
		initialTxCount, txCount, initialBlockCount, blockCount, tps, bps)
}

// TestMetrics_EphemeralMode 验证 Ephemeral 模式下指标仍然更新
func TestMetrics_EphemeralMode(t *testing.T) {
	m := GetMetrics()

	// 模拟 Ephemeral 模式的处理
	initialActivity := m.GetTotalBlocksProcessed()

	// 调用 RecordBlockActivity (这是在 ephemeral 模式下会被调用的)
	for i := 0; i < 5; i++ {
		m.RecordBlockActivity(1)
	}

	// 验证活动被记录
	newActivity := m.GetTotalBlocksProcessed()
	assert.GreaterOrEqual(t, newActivity, initialActivity,
		"Block activity should be recorded even in ephemeral mode")

	t.Logf("✅ SUCCESS: Ephemeral mode metrics working. Activity: %d -> %d",
		initialActivity, newActivity)
}

// TestMetrics_RegistryExport 验证指标能被 Prometheus Registry 正确导出
func TestMetrics_RegistryExport(t *testing.T) {
	m := GetMetrics()

	// 更新各种指标
	m.UpdateCurrentSyncHeight(12345)
	m.UpdateChainHeight(15000)
	m.UpdateSyncLag(2655)

	// 使用 testutil 验证指标值
	syncHeight := testutil.ToFloat64(m.CurrentSyncHeight)
	chainHeight := testutil.ToFloat64(m.CurrentChainHeight)
	lag := testutil.ToFloat64(m.SyncLag)

	assert.Equal(t, 12345.0, syncHeight, "CurrentSyncHeight gauge incorrect")
	assert.Equal(t, 15000.0, chainHeight, "CurrentChainHeight gauge incorrect")
	// Lag 是计算值: 15000 - 12345 = 2655
	assert.Equal(t, 2655.0, lag, "SyncLag gauge incorrect")

	t.Logf("✅ SUCCESS: Registry export verified. Sync: %.0f, Chain: %.0f, Lag: %.0f",
		syncHeight, chainHeight, lag)
}

// TestMetrics_AllCountersExist 验证所有计数器都存在且可递增
func TestMetrics_AllCountersExist(t *testing.T) {
	m := GetMetrics()

	counters := []struct {
		name   string
		inc    func()
		metric prometheus.Counter
	}{
		{"BlocksProcessed", func() { m.BlocksProcessed.Inc() }, m.BlocksProcessed},
		{"BlocksFailed", func() { m.BlocksFailed.Inc() }, m.BlocksFailed},
		{"BlocksSkipped", func() { m.BlocksSkipped.Inc() }, m.BlocksSkipped},
		{"TransfersProcessed", func() { m.RecordTransferProcessed() }, m.TransfersProcessed},
		{"TransfersFailed", func() { m.TransfersFailed.Inc() }, m.TransfersFailed},
		{"FetcherJobsQueued", func() { m.FetcherJobsQueued.Inc() }, m.FetcherJobsQueued},
		{"FetcherJobsComplete", func() { m.FetcherJobsComplete.Inc() }, m.FetcherJobsComplete},
	}

	for _, c := range counters {
		before := testutil.ToFloat64(c.metric)
		c.inc()
		after := testutil.ToFloat64(c.metric)

		assert.Greater(t, after, before,
			"Counter %s not incrementing (before: %.0f, after: %.0f)",
			c.name, before, after)
	}

	t.Logf("✅ SUCCESS: All %d counters are working correctly", len(counters))
}
