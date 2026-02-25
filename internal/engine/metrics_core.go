package engine

import (
	"sync"
	"sync/atomic"

	"web3-indexer-go/internal/monitor"

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
	BroadcastDropped     prometheus.Counter // 📊 新增：广播消息丢弃计数

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
	DBPoolMaxConns      prometheus.Gauge // 🔥 新增：数据库连接池最大连接数
	DBPoolIdleConns     prometheus.Gauge // 🔥 新增：数据库连接池空闲连接数
	DBPoolInUse         prometheus.Gauge // 🔥 新增：数据库连接池使用中连接数

	// 🔥 Anvil Lab Mode metrics
	LabModeEnabled prometheus.Gauge // 新增：Lab Mode 是否启用

	// System metrics
	CheckpointUpdates  prometheus.Counter
	StartTime          prometheus.Gauge
	CurrentSyncHeight  prometheus.Gauge
	CurrentChainHeight prometheus.Gauge // 新增：链头高度
	SyncLag            prometheus.Gauge // 新增：同步滞后
	E2ELatency         prometheus.Gauge // 新增：秒级 E2E 延迟
	RealtimeTPS        prometheus.Gauge // 新增：实时 TPS
	RealtimeBPS        prometheus.Gauge // 🔥 新增：实时 BPS (Blocks Per Second)
	DiskFree           prometheus.Gauge // 🚀 新增：磁盘剩余空间百分比

	// 📊 Deterministic Telemetry
	tpsMonitor *monitor.TPSMonitor
	bpsMonitor *monitor.TPSMonitor // 🔥 新增：块速率监控

	// 📊 交易类型分布
	TransactionTypesTotal *prometheus.CounterVec

	// 📊 代币转账统计（按代币符号）
	TokenTransferVolume *prometheus.CounterVec
	TokenTransferCount  *prometheus.CounterVec

	// 实时性能指标 (用于 Dashboard)
	totalTxProcessed     uint64
	totalBlocksProcessed uint64

	// 🎬 Replay Metrics
	ReplayProgress prometheus.Gauge

	// 🛡️ Self-healing metrics
	SelfHealingTriggered prometheus.Counter
	SelfHealingSuccess   prometheus.Counter
	SelfHealingFailure   prometheus.Counter

	// 📊 内部缓存用于计算 Lag
	lastChainHeight atomic.Int64
	lastSyncHeight  atomic.Int64
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
		BroadcastDropped: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_broadcast_dropped_total",
			Help: "Total number of broadcast messages dropped due to full channel",
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
		DBPoolMaxConns: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_db_pool_max_connections",
			Help: "Maximum database connections configured",
		}),
		DBPoolIdleConns: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_db_pool_idle_connections",
			Help: "Number of idle database connections",
		}),
		DBPoolInUse: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_db_pool_in_use",
			Help: "Number of database connections currently in use",
		}),

		LabModeEnabled: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_lab_mode_enabled",
			Help: "Whether Lab Mode (no hibernation) is enabled (1=enabled, 0=disabled)",
		}),

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
		E2ELatency: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_e2e_latency_seconds",
			Help: "Real-time latency between block timestamp and indexing time",
		}),
		RealtimeTPS: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_realtime_tps",
			Help: "Real-time transactions per second",
		}),
		RealtimeBPS: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_realtime_bps",
			Help: "Real-time blocks per second",
		}),

		tpsMonitor: monitor.NewTPSMonitor(),
		bpsMonitor: monitor.NewTPSMonitor(),

		TransactionTypesTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_transaction_types_total",
			Help: "Total number of transactions by type (deploy, approve, eth_transfer, etc.)",
		}, []string{"type"}),

		// 📊 代币转账统计指标
		TokenTransferVolume: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_token_transfer_volume_total",
			Help: "Total volume of token transfers by token symbol (USDC, DAI, WETH, UNI)",
		}, []string{"symbol"}),
		TokenTransferCount: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_token_transfer_count_total",
			Help: "Total number of token transfers by token symbol",
		}, []string{"symbol"}),

		ReplayProgress: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "indexer_replay_progress_percentage",
			Help: "Current replay progress percentage (0-100)",
		}),

		// 🛡️ Self-healing metrics
		SelfHealingTriggered: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_self_healing_triggered_total",
			Help: "Total number of self-healing attempts triggered",
		}),
		SelfHealingSuccess: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_self_healing_success_total",
			Help: "Total number of successful self-healing operations",
		}),
		SelfHealingFailure: promauto.NewCounter(prometheus.CounterOpts{
			Name: "indexer_self_healing_failure_total",
			Help: "Total number of failed self-healing operations",
		}),
	}
}

// GetTotalBlocksProcessed returns the total number of blocks processed since start
func (m *Metrics) GetTotalBlocksProcessed() uint64 {
	return atomic.LoadUint64(&m.totalBlocksProcessed)
}

// GetTotalTransfersProcessed returns the total number of transfers processed since start
func (m *Metrics) GetTotalTransfersProcessed() uint64 {
	return atomic.LoadUint64(&m.totalTxProcessed)
}

// RecordTokenTransfer 记录单笔代币转账（用于代币统计）
func (m *Metrics) RecordTokenTransfer(symbol string, amount float64) {
	m.TokenTransferVolume.WithLabelValues(symbol).Add(amount)
	m.TokenTransferCount.WithLabelValues(symbol).Inc()
}

// UpdateReplayProgress 更新回放百分比进度
func (m *Metrics) UpdateReplayProgress(percentage float64) {
	if m != nil && m.ReplayProgress != nil {
		m.ReplayProgress.Set(percentage)
	}
}
