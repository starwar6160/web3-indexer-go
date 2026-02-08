package engine

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the indexer
type Metrics struct {
	// Block processing metrics
	BlocksProcessed   prometheus.Counter
	BlocksFailed      prometheus.Counter
	BlocksSkipped     prometheus.Counter
	ProcessingTime    prometheus.Histogram
	ReorgsDetected    prometheus.Counter
	ReorgsHandled     prometheus.Counter

	// Transfer metrics
	TransfersProcessed prometheus.Counter
	TransfersFailed    prometheus.Counter

	// Fetcher metrics
	FetcherJobsQueued   prometheus.Counter
	FetcherJobsComplete prometheus.Counter
	FetcherJobsFailed   prometheus.Counter
	FetcherRateLimited  prometheus.Counter
	FetchTime           prometheus.Histogram

	// Sequencer metrics
	SequencerBufferSize prometheus.Gauge
	SequencerBufferFull prometheus.Counter

	// RPC Pool metrics
	RPCRequestsTotal    *prometheus.CounterVec
	RPCRequestsFailed   *prometheus.CounterVec
	RPCLatency          *prometheus.HistogramVec
	RPCHealthyNodes     *prometheus.GaugeVec

	// Database metrics
	DBConnectionsActive prometheus.Gauge
	DBQueriesTotal      *prometheus.CounterVec
	DBQueryLatency      *prometheus.HistogramVec
	DBErrors            *prometheus.CounterVec

	// System metrics
	CheckpointUpdates   prometheus.Counter
	StartTime           prometheus.Gauge
	CurrentSyncHeight   prometheus.Gauge
}

var (
	metrics     *Metrics
	metricsOnce sync.Once
)

// GetMetrics returns the singleton Metrics instance
func GetMetrics() *Metrics {
	metricsOnce.Do(func() {
		metrics = NewMetrics()
	})
	return metrics
}

// NewMetrics creates a new Metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		BlocksProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_blocks_processed_total",
			Help: "Total number of blocks successfully processed",
		}),
		BlocksFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_blocks_failed_total",
			Help: "Total number of blocks that failed to process",
		}),
		BlocksSkipped: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_blocks_skipped_total",
			Help: "Total number of blocks skipped (duplicates/late)",
		}),
		ProcessingTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "indexer_block_processing_duration_seconds",
			Help:    "Time taken to process a single block",
			Buckets: prometheus.DefBuckets,
		}),
		ReorgsDetected: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_reorgs_detected_total",
			Help: "Total number of reorganizations detected",
		}),
		ReorgsHandled: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_reorgs_handled_total",
			Help: "Total number of reorganizations successfully handled",
		}),

		TransfersProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_transfers_processed_total",
			Help: "Total number of transfer events processed",
		}),
		TransfersFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_transfers_failed_total",
			Help: "Total number of transfer events that failed to process",
		}),

		FetcherJobsQueued: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_fetcher_jobs_queued_total",
			Help: "Total number of jobs queued for fetcher",
		}),
		FetcherJobsComplete: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_fetcher_jobs_completed_total",
			Help: "Total number of jobs completed by fetcher",
		}),
		FetcherJobsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_fetcher_jobs_failed_total",
			Help: "Total number of jobs that failed in fetcher",
		}),
		FetcherRateLimited: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_fetcher_rate_limited_total",
			Help: "Total number of times fetcher was rate limited",
		}),
		FetchTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "indexer_fetch_duration_seconds",
			Help:    "Time taken to fetch a block and its logs",
			Buckets: prometheus.DefBuckets,
		}),

		SequencerBufferSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_sequencer_buffer_size",
			Help: "Current size of sequencer buffer",
		}),
		SequencerBufferFull: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_sequencer_buffer_full_total",
			Help: "Total number of times sequencer buffer was full",
		}),

		RPCRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_rpc_requests_total",
			Help: "Total number of RPC requests by node and method",
		}, []string{"node", "method"}),
		RPCRequestsFailed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_rpc_requests_failed_total",
			Help: "Total number of failed RPC requests by node and method",
		}, []string{"node", "method"}),
		RPCLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "indexer_rpc_request_duration_seconds",
			Help:    "RPC request latency by node and method",
			Buckets: prometheus.DefBuckets,
		}, []string{"node", "method"}),
		RPCHealthyNodes: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "indexer_rpc_healthy_nodes",
			Help: "Number of healthy RPC nodes",
		}, []string{"pool"}),

		DBConnectionsActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_db_connections_active",
			Help: "Number of active database connections",
		}),
		DBQueriesTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_db_queries_total",
			Help: "Total number of database queries by operation",
		}, []string{"operation"}),
		DBQueryLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "indexer_db_query_duration_seconds",
			Help:    "Database query latency by operation",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
		DBErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_db_errors_total",
			Help: "Total number of database errors by operation",
		}, []string{"operation"}),

		CheckpointUpdates: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_checkpoint_updates_total",
			Help: "Total number of checkpoint updates",
		}),
		StartTime: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_start_time_seconds",
			Help: "Unix timestamp when indexer started",
		}),
		CurrentSyncHeight: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_current_sync_height",
			Help: "Current block height being synced",
		}),
	}
}

// RecordBlockProcessed records a successfully processed block
func (m *Metrics) RecordBlockProcessed(duration time.Duration) {
	m.BlocksProcessed.Inc()
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
func (m *Metrics) RecordReorgHandled(blocksAffected int) {
	m.ReorgsHandled.Inc()
}

// RecordTransferProcessed records a processed transfer
func (m *Metrics) RecordTransferProcessed() {
	m.TransfersProcessed.Inc()
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
	labels := prometheus.Labels{"node": node, "method": method}
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
	labels := prometheus.Labels{"operation": operation}
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
