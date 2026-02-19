package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// 🔥 状态检查点系统 (Checkpointing System)
// 核心思想：定期将 Orchestrator 内存状态转储到磁盘,实现秒级冷启动

// Checkpoint 检查点数据结构
type Checkpoint struct {
	Height    uint64           // 检查点高度
	Timestamp time.Time        // 创建时间
	State     CoordinatorState // 完整内存快照
	Checksum  [32]byte         // SHA256 校验和（防止坏块）
	Version   string           // 检查点格式版本
}

// CheckpointService 检查点管理器（记忆）
type CheckpointService struct {
	orchestrator *Orchestrator
	savePath     string   // 检查点保存目录
	interval     uint64   // 每隔多少个块做一次快照
	maxSnapshots int      // 保留最近 N 个快照（滚动备份）
	db           *sqlx.DB // 数据库引用（用于验证一致性）

	mu               sync.RWMutex
	latestCheckpoint *Checkpoint
	lastSaveHeight   uint64
}

// NewCheckpointService 创建检查点服务
func NewCheckpointService(orch *Orchestrator, savePath string, interval uint64, maxSnapshots int, db *sqlx.DB) *CheckpointService {
	return &CheckpointService{
		orchestrator:   orch,
		savePath:       savePath,
		interval:       interval,
		maxSnapshots:   maxSnapshots,
		db:             db,
		lastSaveHeight: 0,
	}
}

// Start 启动检查点服务
func (s *CheckpointService) Start(ctx context.Context) {
	slog.Info("💾 CheckpointService started",
		"save_path", s.savePath,
		"interval", s.interval,
		"max_snapshots", s.maxSnapshots)

	// 确保保存目录存在
	if err := os.MkdirAll(s.savePath, 0755); err != nil {
		slog.Error("💾 Failed to create checkpoint directory", "err", err)
		return
	}

	// 尝试加载最新检查点（秒级热启动）
	if err := s.LoadLatestCheckpoint(); err != nil {
		slog.Warn("💾 No valid checkpoint found, starting from scratch", "err", err)
	}

	// 定期检查点协程
	go s.run(ctx)
}

// run 主循环：定期创建检查点
func (s *CheckpointService) run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每 30 秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			s.maybeCreateCheckpoint()
		}
	}
}

// maybeCreateCheckpoint 条件触发检查点创建
func (s *CheckpointService) maybeCreateCheckpoint() {
	s.mu.RLock()
	currentHeight := s.orchestrator.GetSnapshot().SyncedCursor
	s.mu.RUnlock()

	// 检查是否达到间隔
	if currentHeight < s.lastSaveHeight+s.interval {
		return
	}

	slog.Info("💾 Checkpoint interval reached", "height", currentHeight)
	s.CreateSnapshot(currentHeight)
}

// CreateSnapshot 创建状态快照（异步，不阻塞主循环）
func (s *CheckpointService) CreateSnapshot(currentHeight uint64) {
	// 1. 获取一致性快照
	state := s.orchestrator.GetSnapshot()

	// 2. 🔥 关键：确保数据库已经落盘到此高度（双重一致性）
	// 防止快照指向的块在数据库中不存在
	if state.SyncedCursor < currentHeight {
		slog.Warn("💾 Checkpoint skipped: SyncedCursor < requested height",
			"synced", state.SyncedCursor,
			"requested", currentHeight)
		return
	}

	checkpoint := Checkpoint{
		Height:    state.SyncedCursor,
		Timestamp: time.Now(),
		State:     state,
		Version:   "1.0",
	}

	// 3. 异步保存（不阻塞主循环）
	go s.atomicSave(checkpoint)

	s.lastSaveHeight = currentHeight
}

// atomicSave 原子写入检查点（Write-to-Temp then Rename）
// 这是防止写入中途崩溃导致旧快照损坏的标准做法
func (s *CheckpointService) atomicSave(checkpoint Checkpoint) {
	start := time.Now()

	// 1. 计算校验和
	data, err := s.serializeCheckpoint(checkpoint)
	if err != nil {
		slog.Error("💾 Failed to serialize checkpoint", "err", err)
		return
	}

	hash := sha256.Sum256(data)
	checkpoint.Checksum = hash

	// 2. 写入临时文件
	tempFile := filepath.Join(s.savePath, fmt.Sprintf("temp.ckp.%d", checkpoint.Height))
	if err := s.writeFile(tempFile, checkpoint); err != nil {
		slog.Error("💾 Failed to write temp checkpoint", "err", err)
		return
	}

	// 3. 原子重命名（确保文件系统级别的原子性）
	finalFile := filepath.Join(s.savePath, fmt.Sprintf("checkpoint.ckp.%d", checkpoint.Height))
	if err := os.Rename(tempFile, finalFile); err != nil {
		slog.Error("💾 Failed to rename checkpoint", "err", err)
		os.Remove(tempFile) // 清理临时文件
		return
	}

	// 4. 更新最新检查点引用
	s.mu.Lock()
	s.latestCheckpoint = &checkpoint
	s.mu.Unlock()

	// 5. 滚动清理旧快照
	s.cleanupOldSnapshots()

	slog.Info("💾 Checkpoint saved",
		"height", checkpoint.Height,
		"size_mb", len(data)/1024/1024,
		"duration_ms", time.Since(start).Milliseconds(),
		"checksum", hex.EncodeToString(hash[:8]))
}

// serializeCheckpoint 序列化检查点（使用 gob 二进制格式）
func (s *CheckpointService) serializeCheckpoint(checkpoint Checkpoint) ([]byte, error) {
	// TODO: 考虑迁移到 Protobuf 以获得更好的性能和兼容性
	// gob 优势：Go 原生支持，无需额外依赖
	// Protobuf 优势：跨语言兼容，性能更优，schema 演化友好

	var buf []byte
	return buf, fmt.Errorf("serialization not implemented")
}

// writeFile 写入检查点文件
func (s *CheckpointService) writeFile(path string, checkpoint Checkpoint) error {
	// TODO: 实现实际的文件写入逻辑
	// 建议使用 gob.NewEncoder 写入二进制格式
	return nil
}

// LoadLatestCheckpoint 加载最新有效检查点（秒级热启动）
func (s *CheckpointService) LoadLatestCheckpoint() error {
	// 1. 查找最新检查点文件
	latest, err := s.findLatestValidCheckpoint()
	if err != nil {
		return err
	}

	// 2. 验证校验和
	if err := s.verifyChecksum(latest); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// 3. 验证数据库一致性（双重检查）
	if err := s.verifyDatabaseConsistency(latest); err != nil {
		return fmt.Errorf("database consistency check failed: %w", err)
	}

	// 4. 恢复状态到协调器
	s.orchestrator.RestoreState(latest.State)

	s.mu.Lock()
	s.latestCheckpoint = latest
	s.lastSaveHeight = latest.Height
	s.mu.Unlock()

	slog.Info("🚀 HOT START SUCCESSFUL",
		"height", latest.Height,
		"age", time.Since(latest.Timestamp).String(),
		"transfers", latest.State.Transfers)

	return nil
}

// findLatestValidCheckpoint 查找最新的有效检查点文件
func (s *CheckpointService) findLatestValidCheckpoint() (*Checkpoint, error) {
	// TODO: 扫描 savePath 目录，找到最新的 .ckp 文件
	// 实现逻辑:
	// 1. 列出所有 checkpoint.ckp.* 文件
	// 2. 解析文件名获取高度
	// 3. 选择高度最大的文件
	// 4. 读取并反序列化
	return nil, fmt.Errorf("no checkpoint found")
}

// verifyChecksum 验证检查点校验和
func (s *CheckpointService) verifyChecksum(checkpoint *Checkpoint) error {
	// TODO: 重新序列化并计算 SHA256，与 checkpoint.Checksum 对比
	// 如果不匹配，说明文件损坏，拒绝加载
	return nil
}

// verifyDatabaseConsistency 验证数据库一致性
// 确保检查点指向的块在数据库中真实存在
func (s *CheckpointService) verifyDatabaseConsistency(checkpoint *Checkpoint) error {
	// 查询数据库中最大的块号
	var maxBlock string
	err := s.db.QueryRow(`SELECT MAX(number) FROM blocks`).Scan(&maxBlock)
	if err != nil {
		return err
	}

	if maxBlock == "" {
		return fmt.Errorf("no blocks in database")
	}

	maxBlockNum, ok := new(big.Int).SetString(maxBlock, 10)
	if !ok {
		return fmt.Errorf("invalid block number in database: %s", maxBlock)
	}

	// 检查点高度不能超过数据库实际高度
	if checkpoint.Height > maxBlockNum.Uint64() {
		return fmt.Errorf("checkpoint height %d exceeds database max block %d",
			checkpoint.Height, maxBlockNum.Uint64())
	}

	slog.Debug("💾 Database consistency verified",
		"checkpoint_height", checkpoint.Height,
		"db_max_height", maxBlockNum.Uint64())

	return nil
}

// cleanupOldSnapshots 清理旧快照（滚动备份）
// 保留最近的 maxSnapshots 个快照，删除其余的
func (s *CheckpointService) cleanupOldSnapshots() {
	if s.maxSnapshots <= 0 {
		return // 0 或负数表示不限制
	}

	// TODO: 扫描目录，按创建时间排序，删除超过 maxSnapshots 数量的旧文件
	// 实现逻辑:
	// 1. 列出所有 checkpoint.ckp.* 文件
	// 2. 按修改时间排序（最新的在前）
	// 3. 保留前 maxSnapshots 个，删除其余的
}

// 🔥 针对 4TB 990 PRO 的优化建议

// EnableNVMeOptimization 启用 NVMe 优化
func (s *CheckpointService) EnableNVMeOptimization() {
	// 增大快照间隔（减少写入频率）
	s.interval = 10000 // 每 10000 个块做一次快照（约 30 小时，假设 12s 出块）

	// 增加保留数量（4TB 空间充足）
	s.maxSnapshots = 10

	slog.Info("💾 NVMe optimization enabled",
		"interval", s.interval,
		"max_snapshots", s.maxSnapshots)
}

// SetCompressionLevel 设置压缩级别（利用 3800X 多核性能）
// level: 0 = 不压缩, 1 = 最快, 9 = 最小
func (s *CheckpointService) SetCompressionLevel(level int) {
	// TODO: 使用 lz4 或 zstd 进行快速压缩
	// 压缩虽然消耗 CPU，但能显著减少 I/O 时间
	slog.Info("💾 Compression level set", "level", level)
}

// ExportMetrics 导出监控指标
func (s *CheckpointService) ExportMetrics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := map[string]interface{}{
		"latest_height":    0,
		"latest_timestamp": time.Time{},
		"last_save_height": s.lastSaveHeight,
		"save_path":        s.savePath,
		"interval":         s.interval,
		"max_snapshots":    s.maxSnapshots,
	}

	if s.latestCheckpoint != nil {
		metrics["latest_height"] = s.latestCheckpoint.Height
		metrics["latest_timestamp"] = s.latestCheckpoint.Timestamp
	}

	return metrics
}
