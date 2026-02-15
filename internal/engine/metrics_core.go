package engine

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the indexer
type Metrics struct {
	// Block processing metrics
	BlocksProcessed prometheus.Counter
	BlocksFailed    prometheus.Counter
	BlocksSkipped   prometheus.Counter
	ProcessingTime  prometheus.Histogram
	ReorgsDetected  prometheus.Counter
	ReorgsHandled   prometheus.Counter

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
	RPCRequestsTotal  *prometheus.CounterVec
	RPCRequestsFailed *prometheus.CounterVec
	RPCLatency        *prometheus.HistogramVec
	RPCHealthyNodes   *prometheus.GaugeVec

	// Database metrics
	DBConnectionsActive prometheus.Gauge
	DBQueriesTotal      *prometheus.CounterVec
	DBQueryLatency      *prometheus.HistogramVec
	DBErrors            *prometheus.CounterVec

	// System metrics
	CheckpointUpdates  prometheus.Counter
	StartTime           prometheus.Gauge
	CurrentSyncHeight   prometheus.Gauge
	CurrentChainHeight  prometheus.Gauge // 新增：链头高度
	SyncLag             prometheus.Gauge // 新增：同步滞后
	RealtimeTPS         prometheus.Gauge // 新增：实时 TPS

	// 实时性能指标 (用于 Dashboard)
	totalTxProcessed     uint64
	totalBlocksProcessed uint64
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
		CurrentChainHeight: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_current_chain_height",
			Help: "Current chain head height",
		}),
		SyncLag: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_sync_lag_blocks",
			Help: "Number of blocks behind chain head",
		}),
		RealtimeTPS: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_realtime_tps",
			Help: "Real-time transactions per second",
		}),
	}
}
