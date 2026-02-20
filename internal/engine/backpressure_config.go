package engine

// 🔥 16G 内存优化的背压阈值配置
// 解决 buffer:196 就触发 CRITICAL 的过度敏感问题

import "runtime"

// BackpressureConfig 背压阈值配置
type BackpressureConfig struct {
	// 缓冲队列容量（基于内存大小动态计算）
	MaxJobsCapacity    int
	MaxResultsCapacity int
	MaxSeqBuffer       int

	// 水位线百分比（0-100）
	// 只有当使用率超过此百分比时才触发背压
	JobsWatermarkPercent      int
	ResultsWatermarkPercent   int
	SeqBufferWatermarkPercent int

	// 强制节流阈值（更高）
	// 只有当使用率超过此阈值时才完全停止摄取
	HardLimitPercent int
}

// DefaultBackpressureConfig 返回默认配置（适合 16G 内存）
func DefaultBackpressureConfig() *BackpressureConfig {
	// 根据系统内存动态计算
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 可用内存（字节）
	totalMem := memStats.Sys

	// 对于 16G 内存系统：
	// - Jobs: 160 (原始值)
	// - Results: 15000 (原始值)
	// - SeqBuffer: 5000 (大幅提升，解决196就报critical的问题)

	cfg := &BackpressureConfig{
		MaxJobsCapacity:           160,
		MaxResultsCapacity:        15000,
		MaxSeqBuffer:              5000, // 🔥 从 800 提升到 5000
		JobsWatermarkPercent:      80,   // 80% 时触发警告
		ResultsWatermarkPercent:   80,
		SeqBufferWatermarkPercent: 80,
		HardLimitPercent:          95, // 95% 时完全停止
	}

	// 对于大内存系统，进一步放宽限制
	if totalMem > 8*1024*1024*1024 { // > 8GB
		cfg.MaxSeqBuffer = 10000 // 10K 缓冲区
		cfg.SeqBufferWatermarkPercent = 90
		cfg.HardLimitPercent = 98
	}

	if totalMem > 16*1024*1024*1024 { // > 16GB
		cfg.MaxSeqBuffer = 50000 // 50K 缓冲区
		cfg.SeqBufferWatermarkPercent = 95
		cfg.HardLimitPercent = 99
	}

	return cfg
}

// GetEffectiveThresholds 返回实际阈值（基于容量和百分比）
func (c *BackpressureConfig) GetEffectiveThresholds() (jobs, results, seq, hard int) {
	jobs = c.MaxJobsCapacity * c.JobsWatermarkPercent / 100
	results = c.MaxResultsCapacity * c.ResultsWatermarkPercent / 100
	seq = c.MaxSeqBuffer * c.SeqBufferWatermarkPercent / 100
	hard = c.MaxSeqBuffer * c.HardLimitPercent / 100
	return
}

// ShouldThrottle 检查是否应该节流（背压）
func (c *BackpressureConfig) ShouldThrottle(jobsDepth, resultsDepth, seqDepth int) bool {
	jobsThreshold, resultsThreshold, seqThreshold, _ := c.GetEffectiveThresholds()

	if jobsDepth > jobsThreshold {
		return true
	}
	if resultsDepth > resultsThreshold {
		return true
	}
	if seqDepth > seqThreshold {
		return true
	}
	return false
}

// ShouldHardStop 检查是否应该完全停止
func (c *BackpressureConfig) ShouldHardStop(jobsDepth, resultsDepth, seqDepth int) bool {
	_, _, _, hardLimit := c.GetEffectiveThresholds()

	if jobsDepth > c.MaxJobsCapacity*95/100 {
		return true
	}
	if resultsDepth > c.MaxResultsCapacity*95/100 {
		return true
	}
	if seqDepth > hardLimit {
		return true
	}
	return false
}

// IsNearEmpty 检查是否接近空（可以用于缩容判断）
func (c *BackpressureConfig) IsNearEmpty(jobsDepth, resultsDepth, seqDepth int) bool {
	return jobsDepth < c.MaxJobsCapacity*20/100 &&
		resultsDepth < c.MaxResultsCapacity*20/100 &&
		seqDepth < c.MaxSeqBuffer*20/100
}
