package engine

import (
	"sync/atomic"
	"time"
)

// HeightOracle is the single source of truth for all height-related numbers.
// It eliminates the "数字幽灵" (phantom number) problem caused by three
// independent code paths reading chain height from different sources:
//
//   - TailFollow goroutine: calls GetLatestBlockNumber every 500ms → writes here
//   - /api/status handler: previously called GetLatestBlockNumber live (stale node risk)
//   - Metrics.lastChainHeight: written by UpdateChainHeight, read by recalculateLag
//
// After this change, ALL readers use HeightOracle.ChainHead(), which is always
// the value last observed by TailFollow — the most authoritative source.
//
// Drift detection: if ChainHead < IndexedHead, the indexer has processed blocks
// beyond what the RPC node currently reports (time-travel / node lag). This is
// surfaced as DriftBlocks() > 0 rather than silently clamped to 0.
type HeightOracle struct {
	chainHead   atomic.Int64 // latest block number seen on-chain (from TailFollow)
	indexedHead atomic.Int64 // latest block number committed to DB (from Processor)
	updatedAt   atomic.Int64 // unix nano of last chainHead update

	// StrictHeightCheck: when true, log an error if indexedHead > chainHead + DriftTolerance.
	// Corresponds to advanced_metrics.strict_height_check in config.
	StrictHeightCheck bool
	// DriftTolerance: number of blocks by which indexedHead may exceed chainHead
	// before StrictHeightCheck fires. Accounts for RPC node propagation lag.
	// Corresponds to advanced_metrics.drift_tolerance in config.
	DriftTolerance int64
}

// globalHeightOracle is the process-wide singleton, wired up in initEngine.
var globalHeightOracle = &HeightOracle{
	StrictHeightCheck: true,
	DriftTolerance:    5,
}

// GetHeightOracle returns the process-wide HeightOracle singleton.
func GetHeightOracle() *HeightOracle {
	return globalHeightOracle
}

// SetChainHead records the latest on-chain block number observed by TailFollow.
// This is the only writer for chainHead; all other code must call ChainHead().
func (o *HeightOracle) SetChainHead(height int64) {
	o.chainHead.Store(height)
	o.updatedAt.Store(time.Now().UnixNano())

	// Keep Metrics in sync so Prometheus gauges and recalculateLag() stay accurate.
	GetMetrics().UpdateChainHeight(height)
}

// SetIndexedHead records the latest block number committed to the database.
// Called by Processor after a successful checkpoint update.
func (o *HeightOracle) SetIndexedHead(height int64) {
	o.indexedHead.Store(height)
	GetMetrics().UpdateCurrentSyncHeight(height)

	// Strict height check: warn when the indexer has processed blocks beyond
	// what the RPC node currently reports. This is the "Synced > On-Chain"
	// / "time-travel" condition. It is benign when within DriftTolerance
	// (normal RPC propagation lag), but indicates a stale HeightOracle or
	// node lag when it exceeds the tolerance.
	if o.StrictHeightCheck {
		chain := o.chainHead.Load()
		if chain > 0 && height > chain+o.DriftTolerance {
			Logger.Error("⏰ HEIGHT_ORACLE_TIME_TRAVEL: indexed head exceeds chain head beyond drift tolerance",
				"indexed_head", height,
				"chain_head", chain,
				"drift_blocks", height-chain,
				"drift_tolerance", o.DriftTolerance,
				"action", "TailFollow may be stale; check RPC node health",
			)
		}
	}
}

// ChainHead returns the latest on-chain block number.
func (o *HeightOracle) ChainHead() int64 {
	return o.chainHead.Load()
}

// IndexedHead returns the latest block number committed to the database.
func (o *HeightOracle) IndexedHead() int64 {
	return o.indexedHead.Load()
}

// UpdatedAt returns the time of the last SetChainHead call.
func (o *HeightOracle) UpdatedAt() time.Time {
	return time.Unix(0, o.updatedAt.Load())
}

// SyncLag returns the number of blocks the indexer is behind the chain.
// Returns 0 when caught up. Never returns a negative number.
func (o *HeightOracle) SyncLag() int64 {
	lag := o.chainHead.Load() - o.indexedHead.Load()
	if lag < 0 {
		return 0
	}
	return lag
}

// DriftBlocks returns how many blocks the indexer is AHEAD of the chain head.
// A positive value means the indexer has processed blocks the RPC node has not
// yet reported — this is the "time-travel" / "Synced > On-Chain" condition.
// Returns 0 when indexedHead <= chainHead.
func (o *HeightOracle) DriftBlocks() int64 {
	drift := o.indexedHead.Load() - o.chainHead.Load()
	if drift < 0 {
		return 0
	}
	return drift
}

// IsTimeTravel returns true when the indexer has processed blocks beyond the
// currently reported chain head (DriftBlocks > DriftTolerance).
func (o *HeightOracle) IsTimeTravel() bool {
	return o.DriftBlocks() > o.DriftTolerance
}

// Snapshot returns a consistent point-in-time view of all height values.
// Use this in /api/status to avoid reading chainHead and indexedHead at
// different instants (which would produce a spurious lag or drift reading).
type HeightSnapshot struct {
	ChainHead    int64
	IndexedHead  int64
	SyncLag      int64 // chainHead - indexedHead, clamped to 0
	DriftBlocks  int64 // indexedHead - chainHead, clamped to 0
	IsTimeTravel bool
	UpdatedAt    time.Time
}

// Snapshot atomically captures the current state.
func (o *HeightOracle) Snapshot() HeightSnapshot {
	chain := o.chainHead.Load()
	indexed := o.indexedHead.Load()
	updatedAt := time.Unix(0, o.updatedAt.Load())

	lag := chain - indexed
	drift := int64(0)
	if lag < 0 {
		drift = -lag
		lag = 0
	}

	return HeightSnapshot{
		ChainHead:    chain,
		IndexedHead:  indexed,
		SyncLag:      lag,
		DriftBlocks:  drift,
		IsTimeTravel: drift > o.DriftTolerance,
		UpdatedAt:    updatedAt,
	}
}
