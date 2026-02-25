package engine

import (
	"log/slog"
	"time"
)

// 🔥 核心：调度循环（单一入口点）
// 所有的状态变更都发生在这个协程里，确保逻辑绝对线性
func (o *Orchestrator) loop() {
	slog.Info("🎼 Coordinator: SSOT Engine Online", "location", "Yokohama-Lab-Primary")

	// 🔥 自动化休眠决策引擎：每 5 秒进行一次"自我审查"
	decisionTicker := time.NewTicker(5 * time.Second)
	defer decisionTicker.Stop()

	// 🔥 消息合并定时器：每 100ms 合并一次高度更新（防止 Anvil 高频推送溢出）
	mergeTicker := time.NewTicker(100 * time.Millisecond)
	defer mergeTicker.Stop()

	// 📊 遥测定时器：每 1 秒输出一行 AI 专用诊断日志
	telemetryTicker := time.NewTicker(1 * time.Second)
	defer telemetryTicker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			slog.Warn("🎼 Coordinator: Shutting down...")
			return

		case msg := <-o.cmdChan:
			o.process(msg)

		case <-decisionTicker.C:
			o.evaluateEcoMode()
			o.evaluateSystemState()

		case <-mergeTicker.C:
			o.flushPendingHeightUpdate()

		case <-telemetryTicker.C:
			o.LogPulse(o.ctx)
		}
	}
}

// process 处理消息（状态机核心逻辑）
func (o *Orchestrator) process(msg Message) {
	// 记录每一个状态脉动的处理耗时
	start := time.Now()

	switch msg.Type {
	case CmdUpdateChainHeight:
		h, _ := msg.Data.(uint64)
		if h > o.state.LatestHeight {
			o.pendingHeightUpdate = &h
			o.lastHeightMergeTime = time.Now()
			if h > o.state.SafetyBuffer {
				o.state.TargetHeight = h - o.state.SafetyBuffer
			} else {
				o.state.TargetHeight = 0
			}
		}

	case CmdFetchFailed:
		errType, _ := msg.Data.(string)
		if errType == "not_found" {
			o.state.SuccessCount = 0
			if o.state.SafetyBuffer < 20 {
				o.state.SafetyBuffer++
			}
		}

	case CmdFetchSuccess:
		o.state.SuccessCount++
		if o.state.SuccessCount >= 50 && o.state.SafetyBuffer > 1 {
			o.state.SafetyBuffer--
			o.state.SuccessCount = 0
		}

	case CmdNotifyFetched, CmdNotifyFetchProgress:
		h, ok := msg.Data.(uint64)
		if ok && h > o.state.FetchedHeight {
			o.state.FetchedHeight = h
		}

	case CmdLogEvent:
		logData, ok := msg.Data.(map[string]interface{})
		if ok {
			o.state.LogEntry = logData
			o.state.UpdatedAt = time.Now()
		}

	case CmdCommitBatch:
		task, ok := msg.Data.(PersistTask)
		if ok && o.asyncWriter != nil {
			_ = o.asyncWriter.Enqueue(task)
		}

	case CmdCommitDisk:
		diskHeight, ok := msg.Data.(uint64)
		if ok && diskHeight > o.state.SyncedCursor {
			o.state.SyncedCursor = diskHeight
		}

	case CmdResetCursor:
		resetHeight, ok := msg.Data.(uint64)
		if ok {
			o.state.SyncedCursor = resetHeight
		}

	case CmdIncrementTransfers:
		count, ok := msg.Data.(uint64)
		if ok {
			o.state.Transfers += count
		}

	case CmdToggleEcoMode:
		active, ok := msg.Data.(bool)
		if ok {
			o.state.IsEcoMode = active
		}

	case CmdSetSystemState:
		state, ok := msg.Data.(SystemStateEnum)
		if ok {
			o.state.SystemState = state
		}

	case ReqGetStatus, ReqGetSnapshot:
		if msg.Reply != nil {
			msg.Reply <- o.state
		}
	}

	o.updateProgressAndSnapshot()

	if o.enableProfiling {
		if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
			slog.Warn("🎼 Slow Process", "seq", msg.Sequence, "dur", elapsed, "type", msg.Type)
		}
	}
}

func (o *Orchestrator) updateProgressAndSnapshot() {
	if o.state.LatestHeight > 0 {
		o.state.Progress = (float64(o.state.SyncedCursor) / float64(o.state.LatestHeight)) * 100
		if o.state.Progress > 100.0 {
			o.state.Progress = 100.0
		}
	}
	o.state.UpdatedAt = time.Now()
	o.mu.Lock()
	o.snapshot = o.state
	o.mu.Unlock()
	select {
	case o.broadcastCh <- o.snapshot:
	default:
		// 📊 记录广播消息丢弃，用于监控 channel 满载情况
		GetMetrics().BroadcastDropped.Add(1)
	}
}

// evaluateSystemState 评估系统状态
func (o *Orchestrator) evaluateSystemState() {
	jobsDepth := 0
	resultsDepth := 0
	if o.fetcher != nil {
		jobsDepth = o.fetcher.QueueDepth()
		resultsDepth = o.fetcher.ResultsDepth()
		o.state.JobsDepth = jobsDepth
		o.state.ResultsDepth = resultsDepth
	}

	GetGlobalState().UpdatePipelineDepth(int32(jobsDepth), int32(resultsDepth), 0)
	snap := GetGlobalState().Snapshot()
	
	if snap.ResultsDepth > snap.PipelineDepth*80/100 {
		o.state.SystemState = SystemStateThrottled
		return
	}
	if o.state.SafetyBuffer > 1 {
		o.state.SystemState = SystemStateOptimizing
		return
	}
	if o.state.SystemState == SystemStateOptimizing || o.state.SystemState == SystemStateThrottled || o.state.SystemState == SystemStateUnknown {
		o.state.SystemState = SystemStateRunning
	}
}

// evaluateEcoMode 评估休眠模式
func (o *Orchestrator) evaluateEcoMode() {
	lag := o.state.LatestHeight - o.state.SyncedCursor
	idleTime := time.Since(o.state.LastUserActivity)

	shouldBeEco := false
	if lag <= 10 && idleTime >= 2*time.Minute {
		shouldBeEco = true
	}

	if o.state.IsEcoMode != shouldBeEco {
		o.state.IsEcoMode = shouldBeEco
		slog.Warn("🎼 DecisionEngine: Mode Switch", "to_eco", shouldBeEco, "lag", lag)
	}
}

func (o *Orchestrator) flushPendingHeightUpdate() {
	if o.pendingHeightUpdate != nil {
		h := *o.pendingHeightUpdate
		if h > o.state.LatestHeight {
			o.state.LatestHeight = h
		}
		o.pendingHeightUpdate = nil
	}
}
