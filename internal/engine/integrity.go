package engine

import (
	"context"
	"fmt"
	"log/slog"
)

// AlignInfrastructure 执行启动期的高度对齐自检
// 解决 5600U 实验室环境下 Anvil 重启导致的“时空穿越”和数据断层问题
func (p *Processor) AlignInfrastructure(ctx context.Context, rpcPool RPCClient) error {
	Logger.Info("🛡️ [Integrity] 启动高度对齐自检...")

	rpcHeight, err := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get rpc height: %w", err)
	}

	repo := p.GetRepoAdapter()
	dbHeight, err := repo.GetMaxStoredBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get db height: %w", err)
	}

	Logger.Info("📊 [Integrity] 高度对比",
		slog.String("rpc", rpcHeight.String()),
		slog.Int64("db", dbHeight))

	// 🚀 场景 A：时空穿越 (DB > RPC) - 常见于 Anvil 重置
	if dbHeight > rpcHeight.Int64() {
		diff := dbHeight - rpcHeight.Int64()
		Logger.Warn("🚨 [Integrity] 检测到时空穿越，执行物理剪枝", "surplus", diff)
		// 强制回滚数据库到 RPC 链头高度
		if err := repo.PruneFutureData(ctx, rpcHeight.Int64()); err != nil {
			return fmt.Errorf("critical pruning failure: %w", err)
		}
		Logger.Info("✅ [Integrity] 剪枝成功，数据库已回滚至 RPC 锚点", "new_height", rpcHeight.Int64())
	}

	// 🚀 场景 B：演示模式下的深度追赶 (Gap > 1000)
	// 注意：Processor 需要有 DemoMode 标志
	// 这里假设 Gap > 1000 就需要跳跃
	if rpcHeight.Int64() > dbHeight+1000 {
		Logger.Warn("🚧 [Integrity] 检测到深度断层，演示模式：执行状态坍缩(Jump)")
		if err := repo.UpdateSyncCursor(ctx, rpcHeight.Int64()-1); err != nil {
			return err
		}
		Logger.Info("✅ [Integrity] 状态坍缩完成，游标已对齐")
	}

	Logger.Info("✅ [Integrity] 高度校验通过", "height", rpcHeight.String())
	return nil
}
