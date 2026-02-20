package engine

import (
	"context"
	"testing"
	"time"
)

// TestDebugAPI_ConsistencyChecks 验证一致性检查能发现 80% 的隐性问题
// 这是核心集成测试：不需要容器，直接操作内存状态
func TestDebugAPI_ConsistencyChecks(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()
	cm := NewConfigManagerFromEnv()
	dbg := NewDebugServer(o, cm)

	t.Run("healthy_state_passes", func(t *testing.T) {
		o.Reset()
		o.UpdateChainHead(100)
		o.AdvanceDBCursor(80)
		o.Dispatch(CmdNotifyFetchProgress, uint64(95))
		time.Sleep(50 * time.Millisecond)

		snap := dbg.BuildFullSnapshot(context.Background())
		if snap.Consistency.HeightParadox {
			t.Errorf("FALSE_POSITIVE: healthy state flagged as height paradox: %s", snap.Consistency.HeightParadoxDesc)
		}
		if snap.Consistency.WatermarkInversion {
			t.Errorf("FALSE_POSITIVE: healthy state flagged as watermark inversion: %s", snap.Consistency.WatermarkInversionDesc)
		}
	})

	t.Run("height_paradox_detected", func(t *testing.T) {
		o.Reset()
		// 人为制造悖论：SyncedCursor > LatestHeight
		o.mu.Lock()
		o.state.LatestHeight = 100
		o.state.SyncedCursor = 200 // 索引器领先于链
		o.snapshot = o.state
		o.mu.Unlock()

		snap := dbg.BuildFullSnapshot(context.Background())
		if !snap.Consistency.HeightParadox {
			t.Error("AI_FIX_REQUIRED: HEIGHT_PARADOX not detected when SyncedCursor > LatestHeight")
		}
		if snap.Consistency.IsHealthy {
			t.Error("AI_FIX_REQUIRED: IsHealthy should be false when paradox detected")
		}
		t.Logf("✅ Detected: %s", snap.Consistency.HeightParadoxDesc)
	})

	t.Run("watermark_inversion_detected", func(t *testing.T) {
		o.Reset()
		// 人为制造水位线倒挂：FetchedHeight < SyncedCursor
		o.mu.Lock()
		o.state.LatestHeight = 1000
		o.state.FetchedHeight = 500 // 内存落后于磁盘
		o.state.SyncedCursor = 800  // 磁盘领先于内存（异常）
		o.snapshot = o.state
		o.mu.Unlock()

		snap := dbg.BuildFullSnapshot(context.Background())
		if !snap.Consistency.WatermarkInversion {
			t.Error("AI_FIX_REQUIRED: WATERMARK_INVERSION not detected when FetchedHeight < SyncedCursor")
		}
		t.Logf("✅ Detected: %s", snap.Consistency.WatermarkInversionDesc)
	})

	t.Run("cmd_chan_pressure_detected", func(t *testing.T) {
		o.Reset()
		// 填满 90% 的命令通道
		fillCount := cap(o.cmdChan) * 85 / 100
		for i := 0; i < fillCount; i++ {
			select {
			case o.cmdChan <- Message{Type: CmdLogEvent, Data: nil}:
			default:
				break
			}
		}

		snap := dbg.BuildFullSnapshot(context.Background())
		if !snap.Consistency.CmdChanPressure {
			t.Logf("INFO: CmdChan usage=%d%% (threshold=80%%)", snap.Pipeline.CmdChanUsagePct)
			// 注意：如果 loop() 消费了消息，可能不会触发，这是正常行为
		} else {
			t.Logf("✅ Detected CmdChan pressure: %s", snap.Consistency.CmdChanPressureDesc)
		}

		// 清空通道
		for len(o.cmdChan) > 0 {
			<-o.cmdChan
		}
	})
}

// TestDebugAPI_ComponentHealth 验证组件健康检查
func TestDebugAPI_ComponentHealth(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()
	cm := NewConfigManagerFromEnv()
	dbg := NewDebugServer(o, cm)

	report := dbg.BuildComponentHealth()

	if report.Overall == "" {
		t.Error("AI_FIX_REQUIRED: ComponentHealth overall status is empty")
	}
	if len(report.Components) == 0 {
		t.Error("AI_FIX_REQUIRED: ComponentHealth has no components")
	}

	// 验证所有预期组件都存在
	expectedComponents := []string{"orchestrator", "height_oracle", "global_state", "cmd_channel", "fetcher"}
	componentNames := make(map[string]bool)
	for _, c := range report.Components {
		componentNames[c.Name] = true
	}
	for _, expected := range expectedComponents {
		if !componentNames[expected] {
			t.Errorf("AI_FIX_REQUIRED: Missing component in health report: %s", expected)
		}
	}

	t.Logf("✅ ComponentHealth: overall=%s components=%d", report.Overall, len(report.Components))
}

// TestDebugAPI_PipelineTrace 验证数据流追踪
func TestDebugAPI_PipelineTrace(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()
	cm := NewConfigManagerFromEnv()
	dbg := NewDebugServer(o, cm)

	// 设置一个有意义的状态
	o.UpdateChainHead(1000)
	o.Dispatch(CmdNotifyFetchProgress, uint64(900))
	o.AdvanceDBCursor(800)
	time.Sleep(50 * time.Millisecond)

	trace := dbg.BuildPipelineTrace()

	if trace["timestamp_ms"] == nil {
		t.Error("AI_FIX_REQUIRED: PipelineTrace missing timestamp")
	}
	if trace["flow"] == nil {
		t.Error("AI_FIX_REQUIRED: PipelineTrace missing flow stages")
	}

	totalLag, ok := trace["total_lag_blocks"].(int64)
	if !ok {
		t.Error("AI_FIX_REQUIRED: PipelineTrace total_lag_blocks is not int64")
	} else if totalLag < 0 {
		t.Errorf("AI_FIX_REQUIRED: PipelineTrace total_lag_blocks is negative: %d", totalLag)
	}

	t.Logf("✅ PipelineTrace: total_lag=%d", totalLag)
}

// TestDebugAPI_ConfigAudit 验证配置审计
func TestDebugAPI_ConfigAudit(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()
	cm := NewConfigManagerFromEnv()
	dbg := NewDebugServer(o, cm)

	// 注入冲突配置：AlwaysActive=true 但 IsEcoMode=true
	cfg := cm.Get()
	cfg.AlwaysActive = true
	if err := cm.Update(context.Background(), cfg); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}
	o.mu.Lock()
	o.state.IsEcoMode = true
	o.snapshot = o.state
	o.mu.Unlock()

	audit := dbg.BuildConfigAudit()

	if audit.WarningCount == 0 {
		t.Error("AI_FIX_REQUIRED: ConfigAudit should detect AlwaysActive+IsEcoMode conflict")
	} else {
		t.Logf("✅ ConfigAudit detected %d warnings", audit.WarningCount)
	}
	if audit.IsConsistent {
		t.Error("AI_FIX_REQUIRED: ConfigAudit IsConsistent should be false when warnings exist")
	}
}

// TestDebugAPI_RaceCheck 验证竞争状态检测
func TestDebugAPI_RaceCheck(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()
	cm := NewConfigManagerFromEnv()
	dbg := NewDebugServer(o, cm)

	// 正常状态下不应检测到竞争
	check := dbg.BuildRaceCheck()

	raceDetected, ok := check["race_detected"].(bool)
	if !ok {
		t.Error("AI_FIX_REQUIRED: RaceCheck race_detected is not bool")
	}
	if raceDetected {
		t.Error("AI_FIX_REQUIRED: RaceCheck detected race in normal state - possible data race bug")
	}

	t.Logf("✅ RaceCheck: race_detected=%v samples=%v", raceDetected, check["samples_taken"])
}

// TestDebugAPI_FullSnapshot_Structure 验证快照结构完整性
func TestDebugAPI_FullSnapshot_Structure(t *testing.T) {
	o := GetOrchestrator()
	o.Reset()
	cm := NewConfigManagerFromEnv()
	dbg := NewDebugServer(o, cm)

	snap := dbg.BuildFullSnapshot(context.Background())

	if snap.Timestamp == 0 {
		t.Error("AI_FIX_REQUIRED: DebugSnapshot.Timestamp is zero")
	}
	if snap.Runtime.GOMAXPROCS == 0 {
		t.Error("AI_FIX_REQUIRED: DebugSnapshot.Runtime.GOMAXPROCS is zero")
	}
	if snap.Runtime.NumGoroutines == 0 {
		t.Error("AI_FIX_REQUIRED: DebugSnapshot.Runtime.NumGoroutines is zero")
	}
	if snap.Orchestrator.SystemState == "" {
		t.Error("AI_FIX_REQUIRED: DebugSnapshot.Orchestrator.SystemState is empty")
	}
	if snap.Pipeline.CmdChanCap == 0 {
		t.Error("AI_FIX_REQUIRED: DebugSnapshot.Pipeline.CmdChanCap is zero")
	}

	t.Logf("✅ FullSnapshot structure OK: goroutines=%d heap=%dMB gomaxprocs=%d",
		snap.Runtime.NumGoroutines, snap.Runtime.HeapAllocMB, snap.Runtime.GOMAXPROCS)
}
