package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
)

// Dispatch 发送异步命令（非阻塞）
func (o *Orchestrator) Dispatch(t MsgType, data interface{}) uint64 {
	seq := atomic.AddUint64(&o.msgSeq, 1)
	msg := Message{Type: t, Data: data, Sequence: seq}

	select {
	case <-o.ctx.Done():
		slog.Warn("dispatch_dropped_after_shutdown", "seq", seq, "type", t.String())
		return seq
	case o.cmdChan <- msg:
		return seq
	default:
		slog.Error("orchestrator_command_channel_full", "seq", seq, "type", t.String())
		return seq
	}
}

// DispatchSync 发送同步查询（阻塞）
func (o *Orchestrator) DispatchSync(t MsgType, data interface{}) (interface{}, error) {
	seq := atomic.AddUint64(&o.msgSeq, 1)
	replyCh := make(chan interface{}, 1)
	msg := Message{Type: t, Data: data, Reply: replyCh, Sequence: seq}

	select {
	case o.cmdChan <- msg:
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("dispatch timeout: seq=%d", seq)
	}

	select {
	case result := <-replyCh:
		return result, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("reply timeout: seq=%d", seq)
	}
}

// GetSnapshot 获取状态快照（极速、无阻塞）
func (o *Orchestrator) GetSnapshot() CoordinatorState {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.snapshot
}

// Subscribe 订阅状态快照
func (o *Orchestrator) Subscribe() <-chan CoordinatorState {
	ch := make(chan CoordinatorState, 100)
	o.subscribersMu.Lock()
	o.subscribers = append(o.subscribers, ch)
	total := len(o.subscribers)
	o.subscribersMu.Unlock()

	slog.Info("orchestrator_subscriber_registered", "total", total)
	return ch
}

// UpdateChainHead 更新链头高度
func (o *Orchestrator) UpdateChainHead(height uint64) {
	o.mu.Lock()
	if height > o.state.LatestHeight {
		o.state.LatestHeight = height
		if height > o.state.SafetyBuffer {
			o.state.TargetHeight = height - o.state.SafetyBuffer
		} else {
			o.state.TargetHeight = 0
		}
		o.snapshot = o.state
		o.state.UpdatedAt = time.Now()
	}
	o.mu.Unlock()
}

// AdvanceDBCursor 前进数据库游标
func (o *Orchestrator) AdvanceDBCursor(height uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if height > o.state.SyncedCursor {
		o.state.SyncedCursor = height
		slog.Info("🎼 Orchestrator: Synced cursor advanced", "height", height)
	}
}

// SetSystemState 设置系统状态
func (o *Orchestrator) SetSystemState(state SystemStateEnum) {
	o.Dispatch(CmdSetSystemState, state)
}

// GetSyncLag 获取同步滞后
func (o *Orchestrator) GetSyncLag() int64 {
	snap := o.GetSnapshot()
	if snap.LatestHeight == 0 {
		return 0
	}
	lag := int64(snap.LatestHeight) - int64(snap.SyncedCursor)
	if lag < 0 {
		return 0
	}
	return lag
}

// GetStatus 返回全面的 API 响应 Map
func (o *Orchestrator) GetStatus(ctx context.Context, db *sqlx.DB, rpcPool RPCClient, version string) map[string]interface{} {
	snap := o.GetSnapshot()
	syncLag := SafeInt64Diff(snap.LatestHeight, snap.SyncedCursor)
	if syncLag < 0 {
		syncLag = 0
	}

	fetchProgress := 0.0
	if snap.LatestHeight > 0 {
		fetchProgress = float64(snap.FetchedHeight) / float64(snap.LatestHeight) * 100
		if fetchProgress > 100.0 {
			fetchProgress = 100.0
		}
	}

	status := map[string]interface{}{
		"version":        version,
		"state":          snap.SystemState.String(),
		"latest_block":   fmt.Sprintf("%d", snap.LatestHeight),
		"target_height":  fmt.Sprintf("%d", snap.TargetHeight),
		"latest_fetched": fmt.Sprintf("%d", snap.FetchedHeight),
		"fetch_progress": fetchProgress,
		"safety_buffer":  snap.SafetyBuffer,
		"latest_indexed": fmt.Sprintf("%d", snap.SyncedCursor),
		"sync_lag":       syncLag,
		"transfers":      snap.Transfers,
		"is_eco_mode":    snap.IsEcoMode,
		"progress":       snap.Progress,
		"updated_at":     snap.UpdatedAt.Format(time.RFC3339),
		"is_healthy":     rpcPool.GetHealthyNodeCount() > 0,
		"rpc_nodes": map[string]int{
			"healthy": rpcPool.GetHealthyNodeCount(),
			"total":   rpcPool.GetTotalNodeCount(),
		},
		"jobs_depth":       snap.JobsDepth,
		"results_depth":    snap.ResultsDepth,
		"jobs_capacity":    160,
		"results_capacity": 15000,
		"tps":              GetMetrics().GetWindowTPS(),
		"bps":              GetMetrics().GetWindowBPS(),
	}

	if o.asyncWriter != nil {
		writerMetrics := o.asyncWriter.GetMetrics()
		for k, v := range writerMetrics {
			status["writer_"+k] = v
		}
	}
	return status
}

// broadcaster 广播快照协程
func (o *Orchestrator) broadcaster() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastSnapshot CoordinatorState
	for {
		select {
		case <-o.ctx.Done():
			return
		case snapshot := <-o.broadcastCh:
			lastSnapshot = snapshot
		case <-ticker.C:
			if len(o.subscribers) > 0 {
				o.broadcastSnapshot(lastSnapshot)
			}
		}
	}
}

func (o *Orchestrator) broadcastSnapshot(snapshot CoordinatorState) {
	o.subscribersMu.RLock()
	subscribers := append([]chan CoordinatorState(nil), o.subscribers...)
	o.subscribersMu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

func (o *Orchestrator) RecordUserActivity() {
	o.Dispatch(CmdRecordUserActivity, nil)
}

func (o *Orchestrator) DispatchLog(level string, message string, args ...interface{}) {
	data := map[string]interface{}{
		"level":   level,
		"msg":     message,
		"ts":      time.Now().Unix(),
		"details": args,
	}
	o.Dispatch(CmdLogEvent, data)
}
