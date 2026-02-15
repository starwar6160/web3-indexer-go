package engine

import (
	"sync/atomic"
	"time"
)

// RecordBlockProcessed records a successfully processed block
func (m *Metrics) RecordBlockProcessed(duration time.Duration) {
	m.BlocksProcessed.Inc()
	atomic.AddUint64(&m.totalBlocksProcessed, 1)
	m.ProcessingTime.Observe(duration.Seconds())
}

// RecordBlockFailed records a failed block processing
func (m *Metrics) RecordBlockFailed() {
	m.BlocksFailed.Inc()
}

// RecordBlockSkipped records a skipped block
func (m *Metrics) RecordBlockSkipped() {
	m.BlocksSkipped.Inc()
}

// RecordReorgDetected records a reorganization detection
func (m *Metrics) RecordReorgDetected() {
	m.ReorgsDetected.Inc()
}

// RecordReorgHandled records a successful reorg handling
func (m *Metrics) RecordReorgHandled(_ int) {
	m.ReorgsHandled.Inc()
}

// RecordTransferProcessed records a processed transfer
func (m *Metrics) RecordTransferProcessed() {
	m.TransfersProcessed.Inc()
	atomic.AddUint64(&m.totalTxProcessed, 1)
}

// RecordTransferFailed records a failed transfer
func (m *Metrics) RecordTransferFailed() {
	m.TransfersFailed.Inc()
}

// RecordFetcherJobQueued records a queued fetcher job
func (m *Metrics) RecordFetcherJobQueued() {
	m.FetcherJobsQueued.Inc()
}

// RecordFetcherJobCompleted records a completed fetcher job
func (m *Metrics) RecordFetcherJobCompleted(duration time.Duration) {
	m.FetcherJobsComplete.Inc()
	m.FetchTime.Observe(duration.Seconds())
}

// RecordFetcherJobFailed records a failed fetcher job
func (m *Metrics) RecordFetcherJobFailed() {
	m.FetcherJobsFailed.Inc()
}

// RecordFetcherRateLimited records a rate limiting event
func (m *Metrics) RecordFetcherRateLimited() {
	m.FetcherRateLimited.Inc()
}

// UpdateSequencerBufferSize updates the sequencer buffer size gauge
func (m *Metrics) UpdateSequencerBufferSize(size int) {
	m.SequencerBufferSize.Set(float64(size))
}

// RecordSequencerBufferFull records a buffer overflow event
func (m *Metrics) RecordSequencerBufferFull() {
	m.SequencerBufferFull.Inc()
}

// RecordRPCRequest records an RPC request
func (m *Metrics) RecordRPCRequest(node, method string, duration time.Duration, success bool) {
	labels := map[string]string{"node": node, "method": method}
	m.RPCRequestsTotal.With(labels).Inc()
	m.RPCLatency.With(labels).Observe(duration.Seconds())

	if !success {
		m.RPCRequestsFailed.With(labels).Inc()
	}
}

// UpdateRPCHealthyNodes updates the healthy nodes count for a pool
func (m *Metrics) UpdateRPCHealthyNodes(pool string, count int) {
	m.RPCHealthyNodes.WithLabelValues(pool).Set(float64(count))
}

// UpdateDBConnections updates the active DB connections gauge
func (m *Metrics) UpdateDBConnections(count int) {
	m.DBConnectionsActive.Set(float64(count))
}

// RecordDBQuery records a database query
func (m *Metrics) RecordDBQuery(operation string, duration time.Duration, success bool) {
	labels := map[string]string{"operation": operation}
	m.DBQueriesTotal.With(labels).Inc()
	m.DBQueryLatency.With(labels).Observe(duration.Seconds())

	if !success {
		m.DBErrors.With(labels).Inc()
	}
}

// RecordCheckpointUpdate records a checkpoint update
func (m *Metrics) RecordCheckpointUpdate() {
	m.CheckpointUpdates.Inc()
}

// RecordStartTime records the indexer start time
func (m *Metrics) RecordStartTime() {
	m.StartTime.SetToCurrentTime()
}

// UpdateCurrentSyncHeight updates the current sync height gauge
func (m *Metrics) UpdateCurrentSyncHeight(height int64) {
	m.CurrentSyncHeight.Set(float64(height))
}

// GetSnapshot 获取用于计算 TPS 的快照
func (m *Metrics) GetSnapshot() (sent, confirmed, failed, selfHealed uint64) {
	return atomic.LoadUint64(&m.totalTxProcessed), atomic.LoadUint64(&m.totalBlocksProcessed), 0, 0
}

// UpdateChainHeight 更新链头高度指标
func (m *Metrics) UpdateChainHeight(height int64) {
	m.CurrentChainHeight.Set(float64(height))
}

// UpdateSyncLag 更新同步滞后指标
func (m *Metrics) UpdateSyncLag(lag int64) {
	m.SyncLag.Set(float64(lag))
}

// UpdateE2ELatency 更新 E2E 延迟指标 (秒)
func (m *Metrics) UpdateE2ELatency(seconds float64) {
	m.E2ELatency.Set(seconds)
}

// UpdateRealtimeTPS 更新实时 TPS 指标
func (m *Metrics) UpdateRealtimeTPS(tps float64) {
	m.RealtimeTPS.Set(tps)
}
