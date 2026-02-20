package engine

// orchestrator_handlers.go
// 消息处理器：所有 handle* 方法从 orchestrator.go 提取到此文件
// 职责：纯粹的消息→状态变更逻辑，无 I/O、无外部调用

import (
	"fmt"
	"log/slog"
	"time"
)

// handleMessage 消息分发（状态机入口）
func (o *Orchestrator) handleMessage(msg Message) {
	switch msg.Type {
	case CmdUpdateChainHeight:
		o.handleUpdateChainHeight(msg)
	case CmdFetchFailed:
		o.handleFetchFailed(msg)
	case CmdFetchSuccess:
		o.handleFetchSuccess()
	case CmdNotifyFetched, CmdNotifyFetchProgress:
		o.handleNotifyFetched(msg)
	case CmdLogEvent:
		o.handleLogEvent(msg)
	case CmdCommitBatch:
		o.handleCommitBatch(msg)
	case CmdCommitDisk:
		o.handleCommitDisk(msg)
	case CmdResetCursor:
		o.handleResetCursor(msg)
	case CmdIncrementTransfers:
		o.handleIncrementTransfers(msg)
	case CmdToggleEcoMode:
		o.handleToggleEcoMode(msg)
	case CmdSetSystemState:
		o.handleSetSystemState(msg)
	case ReqGetStatus, ReqGetSnapshot:
		o.handleReply(msg)
	}
}

func (o *Orchestrator) handleUpdateChainHeight(msg Message) {
	h, ok := msg.Data.(uint64)
	if !ok {
		slog.Error("🎼 Orchestrator: Invalid height data type", "type", fmt.Sprintf("%T", msg.Data))
		return
	}
	if h <= o.state.LatestHeight {
		return
	}
	o.pendingHeightUpdate = &h
	o.lastHeightMergeTime = time.Now()
	if h > o.state.SafetyBuffer {
		o.state.TargetHeight = h - o.state.SafetyBuffer
	} else {
		o.state.TargetHeight = 0
	}
	slog.Debug("🎼 Height update cached", "val", h, "target", o.state.TargetHeight, "seq", msg.Sequence)
}

func (o *Orchestrator) handleFetchFailed(msg Message) {
	errType, ok := msg.Data.(string)
	if !ok || errType != "not_found" {
		return
	}
	o.state.SuccessCount = 0
	if o.state.SafetyBuffer < 20 {
		o.state.SafetyBuffer++
		slog.Warn("🎼 Safety: Increasing buffer due to 404", "new_val", o.state.SafetyBuffer)
	}
}

func (o *Orchestrator) handleFetchSuccess() {
	o.state.SuccessCount++
	if o.state.SuccessCount >= 50 && o.state.SafetyBuffer > 1 {
		o.state.SafetyBuffer--
		o.state.SuccessCount = 0
		slog.Info("🎼 Safety: Gradually reducing buffer", "new_val", o.state.SafetyBuffer)
	}
}

func (o *Orchestrator) handleNotifyFetched(msg Message) {
	h, ok := msg.Data.(uint64)
	if ok && h > o.state.FetchedHeight {
		o.state.FetchedHeight = h
	}
}

func (o *Orchestrator) handleLogEvent(msg Message) {
	logData, ok := msg.Data.(map[string]interface{})
	if !ok {
		return
	}
	o.state.LogEntry = logData
	o.state.UpdatedAt = time.Now()
}

func (o *Orchestrator) handleCommitBatch(msg Message) {
	task, ok := msg.Data.(PersistTask)
	if !ok {
		slog.Error("🎼 Orchestrator: Invalid PersistTask data type", "type", fmt.Sprintf("%T", msg.Data))
		return
	}
	if o.asyncWriter != nil {
		if err := o.asyncWriter.Enqueue(task); err != nil {
			slog.Error("🎼 Orchestrator: AsyncWriter enqueue failed", "err", err, "height", task.Height)
		}
	}
	slog.Debug("🎼 State: Logical Commit", "height", task.Height, "seq", msg.Sequence)
}

func (o *Orchestrator) handleCommitDisk(msg Message) {
	diskHeight, ok := msg.Data.(uint64)
	if !ok {
		return
	}
	if diskHeight > o.state.SyncedCursor {
		o.state.SyncedCursor = diskHeight
		slog.Info("🎼 StateChange: Synced (Disk)", "cursor", diskHeight, "seq", msg.Sequence)
	}
}

func (o *Orchestrator) handleResetCursor(msg Message) {
	resetHeight, ok := msg.Data.(uint64)
	if !ok {
		return
	}
	o.state.SyncedCursor = resetHeight
	slog.Warn("🎼 StateChange: Cursor RESET (Reorg)", "to", resetHeight, "seq", msg.Sequence)
}

func (o *Orchestrator) handleIncrementTransfers(msg Message) {
	count, ok := msg.Data.(uint64)
	if !ok {
		return
	}
	o.state.Transfers += count
	slog.Debug("🎼 StateChange: Transfers", "count", count, "total", o.state.Transfers, "seq", msg.Sequence)
}

func (o *Orchestrator) handleToggleEcoMode(msg Message) {
	active, ok := msg.Data.(bool)
	if !ok {
		return
	}
	o.state.IsEcoMode = active
	slog.Warn("🎼 StateChange: EcoMode", "active", active, "seq", msg.Sequence)
}

func (o *Orchestrator) handleSetSystemState(msg Message) {
	state, ok := msg.Data.(SystemStateEnum)
	if !ok {
		return
	}
	o.state.SystemState = state
	slog.Info("🎼 StateChange: SystemState", "state", state.String(), "seq", msg.Sequence)
}

func (o *Orchestrator) handleReply(msg Message) {
	if msg.Reply != nil {
		msg.Reply <- o.state
	}
}
