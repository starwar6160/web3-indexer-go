package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetrics_Recorders(t *testing.T) {
	m := GetMetrics()
	assert.NotNil(t, m)

	// Record various metrics to ensure no panics and some coverage
	m.RecordBlockProcessed(100 * time.Millisecond)
	m.RecordBlockFailed()
	m.RecordBlockSkipped()
	m.RecordReorgDetected()
	m.RecordReorgHandled(5)
	m.RecordTransferProcessed()
	m.RecordTransferFailed()
	m.RecordFetcherJobQueued()
	m.RecordFetcherJobCompleted(50 * time.Millisecond)
	m.RecordFetcherJobFailed()
	m.RecordFetcherRateLimited()
	m.UpdateSequencerBufferSize(10)
	m.RecordSequencerBufferFull()
	m.RecordRPCRequest("node1", "BlockByNumber", 10*time.Millisecond, true)
	m.RecordRPCRequest("node1", "HeaderByNumber", 5*time.Millisecond, false)
	m.UpdateRPCHealthyNodes("default", 5)
	m.UpdateDBConnections(2)
	m.RecordDBQuery("SELECT", 2*time.Millisecond, true)
	m.RecordCheckpointUpdate()
	m.RecordStartTime()
	m.UpdateCurrentSyncHeight(123456)

	txProcessed, blocksProcessed, _, _ := m.GetSnapshot()
	assert.GreaterOrEqual(t, txProcessed, uint64(0))
	assert.GreaterOrEqual(t, blocksProcessed, uint64(1))
}
