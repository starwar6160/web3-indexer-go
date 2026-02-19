package engine

import (
	"context"
	"log/slog"
	"time"
)

// SelfHealer 负责审计和修复内存状态与磁盘状态的偏差
type SelfHealer struct {
	orchestrator *Orchestrator
	interval     time.Duration
}

func NewSelfHealer(o *Orchestrator) *SelfHealer {
	return &SelfHealer{
		orchestrator: o,
		interval:     5 * time.Second,
	}
}

func (s *SelfHealer) Start(ctx context.Context) {
	slog.Info("🛡️ SelfHealer: Audit engine started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.auditAndHeal()
		}
	}
}

func (s *SelfHealer) auditAndHeal() {
	o := s.orchestrator
	o.mu.Lock()
	defer o.mu.Unlock()

	// 1. 物理顺序审计：MemorySync (FetchedHeight) >= DiskSync (SyncedCursor)
	// 如果由于重启或竞态导致内存水位落后，强制对齐
	if o.state.FetchedHeight < o.state.SyncedCursor {
		slog.Warn("🚨 SELF_HEAL: Detecting watermark inversion. Aligning Memory with Disk.",
			"old_mem", o.state.FetchedHeight,
			"new_mem", o.state.SyncedCursor)
		
		o.state.FetchedHeight = o.state.SyncedCursor
	}

	// 2. 边界检查：LatestHeight 绝不应小于 SyncedCursor
	// 常见于 Anvil 重置高度
	if o.state.LatestHeight > 0 && o.state.LatestHeight < o.state.SyncedCursor {
		slog.Error("🚨 SELF_HEAL: Chain height reset detected! Latest < Synced.",
			"chain", o.state.LatestHeight,
			"synced", o.state.SyncedCursor)
		// 注意：这里我们不自动删除数据，交给 ConsistencyGuard 处理
		// 但我们会切换系统状态为 Stalled 提醒 UI
		o.state.SystemState = SystemStateStalled
	}

	// 3. 活跃度审计：如果 Jobs 满了持续过久，可能存在 Fetcher 僵死
	if o.state.JobsDepth >= 150 && o.state.SystemState != SystemStateDegraded {
		slog.Error("🚨 [CRITICAL] SELF_HEAL: Heavy backpressure detected. Suggesting Fetcher audit.")
	}
}
