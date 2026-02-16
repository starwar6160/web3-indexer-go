package engine

import (
	"math"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

// TPSMonitor 计算基于区块时间戳的真实链上 TPS
type TPSMonitor struct {
	lastBlockTime uint64
	lastTPS        float64
}

// NewTPSMonitor 创建 TPS 监控器
func NewTPSMonitor() *TPSMonitor {
	return &TPSMonitor{}
}

// CalculateChainTPS 计算真实的链上 TPS（基于区块时间戳）
//
// 计算公式：当前区块交易数 / 时间差（秒）
//
// 示例：
//   - 区块 N 有 180 笔交易，时间戳 1234567890
//   - 区块 N+1 有 200 笔交易，时间戳 1234567902
//   - 时间差 = 12 秒
//   - TPS = 200 / 12 = 16.67 tx/s
func (m *TPSMonitor) CalculateChainTPS(currentBlock *types.Block, txCount int) float64 {
	// 1. 获取当前块时间戳
	currentTime := currentBlock.Time()

	// 2. 如果是第一块，无法计算差值，返回 0
	if m.lastBlockTime == 0 {
		m.lastBlockTime = currentTime
		return 0.0
	}

	// 3. 计算时间差（秒）
	duration := currentTime - m.lastBlockTime

	// 更新最后时间戳
	m.lastBlockTime = currentTime

	// 防止除以零（在某些私链或测试网可能出现同秒块）
	if duration == 0 {
		// 同一秒内的多个块，使用上一个 TPS 值
		return m.lastTPS
	}

	// 4. 计算真实 TPS
	rawTPS := float64(txCount) / float64(duration)

	// 保留 2 位小数
	tps := math.Round(rawTPS*100) / 100
	m.lastTPS = tps

	return tps
}

// CalculateIngestionRate 计算索引器的数据处理速度（入库速度）
//
// 计算公式：处理记录数 / 处理耗时（秒）
//
// 注意：这个指标反映的是"Go 引擎吞数据的速度"，不是"链上负载"
func CalculateIngestionRate(recordCount int, duration time.Duration) float64 {
	if duration.Seconds() == 0 {
		return 0.0
	}

	rawRate := float64(recordCount) / duration.Seconds()
	return math.Round(rawRate*100) / 100
}
