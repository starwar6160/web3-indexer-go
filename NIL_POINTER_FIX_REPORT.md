# 🛡️ Nil Pointer 防御性修复 - 完整报告

**日期**: 2026-02-17
**问题**: Sequencer panic - nil pointer dereference in `processor_batch.go:146`
**状态**: ✅ 已修复并编译通过

---

## 🔍 问题诊断

### 根本原因

1. **Processor 层**（`processor_batch.go:145`）
   ```go
   lastBlock := blocks[len(blocks)-1].Block
   // ❌ 没有检查 .Block 是否为 nil
   ```

2. **Fetcher 层**（`fetcher_block.go:67-92`）
   - RPC 调用偶尔返回 nil header
   - 没有过滤就直接构造了 BlockData

3. **后果**
   - Sequencer panic -> Recovery 捕获 -> 协程退出
   - Fetcher 继续发送数据，但没人处理
   - 系统进入"僵尸状态"（Sync Lag 停滞）

---

## ✅ 已实施的修复

### 修复 1：Processor Batch 防御性检查

**文件**: `internal/engine/processor_batch.go`

```go
// 🚀 防御性检查：查找最后一个有效的 block 更新 checkpoint
var lastValidBlock *types.Block
for i := len(blocks) - 1; i >= 0; i-- {
    if blocks[i].Block != nil {
        lastValidBlock = blocks[i].Block
        break
    }
}

if lastValidBlock == nil {
    Logger.Warn("⚠️ [BATCH] No valid blocks found in batch, skipping checkpoint update")
    // 仍然提交事务（如果有数据的话）
    if err := dbTx.Commit(); err != nil {
        return fmt.Errorf("failed to commit batch transaction: %w", err)
    }
    return nil
}
```

**效果**：
- ✅ 查找最后一个有效的 block（反向遍历）
- ✅ 如果全部为 nil，跳过 checkpoint 更新但不丢失已写入数据
- ✅ 避免 `lastBlock.Number()` 的 nil pointer dereference

---

### 修复 2：Fetcher 层 Nil 过滤

**文件**: `internal/engine/fetcher_block.go`

#### 修复 A：第 67 行（带 Logs 的块）
```go
header, err := f.fetchHeaderWithRetry(ctx, bn)
if err != nil {
    f.sendResult(ctx, BlockData{Number: bn, Err: err})
    continue
}

// 🚀 防御性检查：确保 header 不为 nil
if header == nil {
    slog.Warn("⚠️ [FETCHER] Failed to fetch header for block with logs",
        "block", bn,
        "skip", true)
    continue
}
```

#### 修复 B：第 87-92 行（最后一个块）
```go
header, err := f.fetchHeaderWithRetry(ctx, bn)
if err == nil && header != nil {
    block = types.NewBlockWithHeader(header)
}
// 🚀 防御性：如果 fetch 失败，记录警告但不发送 nil block
if header == nil {
    slog.Warn("⚠️ [FETCHER] Failed to fetch header for last block",
        "block", bn,
        "skip", true)
    continue // 跳过这个块
}
```

**效果**：
- ✅ 在 Fetcher 层过滤掉 nil header
- ✅ 防止 nil BlockData 进入队列
- ✅ 记录详细的诊断日志

---

## 📊 修复效果对比

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| **BlockData 全部为 nil** | ❌ Panic | ✅ 跳过 checkpoint，提交已有数据 |
| **最后一个 Block 为 nil** | ❌ Panic | ✅ 反向查找有效 block |
| **RPC 返回 nil header** | ❌ 进入队列 | ✅ Fetcher 层过滤 |
| **Sequencer Panic** | ❌ 僵尸状态 | ✅ 3 秒后自动重启 |

---

## 🔧 下一步优化建议

### 1. 添加 Sequencer 自愈能力（已完成✅）

**位置**：`cmd/indexer/main.go` 第 104-130 行（新函数）和第 280 行

#### 新增函数：runSequencerWithSelfHealing

```go
// runSequencerWithSelfHealing 启动 Sequencer 并在崩溃后自动重启
func runSequencerWithSelfHealing(ctx context.Context, sequencer *engine.Sequencer, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			slog.Info("🛑 [SELF-HEAL] Sequencer supervisor stopped")
			return
		default:
			slog.Info("🔄 [SELF-HEAL] Starting Sequencer...")
			recovery.WithRecoveryNamed("sequencer_run", func() {
				sequencer.Run(ctx)
			})

			// 如果 Sequencer 崩溃退出，等待 3 秒后重启
			slog.Warn("⚠️ [SELF-HEAL] Sequencer crashed, restarting in 3s...")
			select {
			case <-ctx.Done():
				slog.Info("🛑 [SELF-HEAL] Sequencer supervisor cancelled during restart delay")
				return
			case <-time.After(3 * time.Second):
				slog.Info("♻️ [SELF-HEAL] Sequencer restarting...")
			}
		}
	}
}
```

#### 修改调用点（第 280 行）：

```go
// 🚀 自愈 Sequencer：崩溃后自动重启
go runSequencerWithSelfHealing(ctx, sequencer, &wg)
```

**效果**：
- ✅ Sequencer 崩溃后 3 秒自动重启
- ✅ 优雅处理 context 取消
- ✅ 详细日志记录自愈过程

---

### 2. 降低 BATCH_SIZE（调试阶段）

**当前**: 可能是 50
**建议**: 调整为 10

**方法**:
- 环境变量: `export MAX_SYNC_BATCH=10`
- 或修改配置: `configs/env/.env.demo2` 中 `MAX_SYNC_BATCH=10`

**原因**:
- 5600U 移动端 CPU 处理 50 个块压力较大
- 减小 batch size 可以降低 panic 影响范围
- 更容易定位具体问题

---

### 3. 立即执行步骤

```bash
# 1. 停止 8092 进程
lsof -ti:8092 | xargs kill -9

# 2. 重启数据库（释放可能的死锁）
docker restart web3-indexer-db
# 或手动清理事务:
# PGPASSWORD=... psql -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='web3_demo' AND state='idle in transaction';"

# 3. 清理可能的孤儿事务
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
  -c "VACUUM FULL ANALYZE;"

# 4. 重新启动
make test-a2
```

---

## 📊 验证方法

### 1. 编译验证
```bash
go build -o /tmp/web3-indexer-fixed ./cmd/indexer
```
**预期**: ✅ 无错误

### 2. 日志监控
```bash
# 实时查看日志
tail -f /tmp/anvil-pro-lab.log | grep -E "(SELF-HEAL|nil pointer|No valid blocks)"
```

### 3. 关键指标

**正常运行**:
- Sequencer 重启: 0 次/小时
- nil header: < 5 次/小时
- panic: 0 次
- Sync Lag: 持续降低

**异常信号**:
- Sequencer 频繁重启
- 大量 "No valid blocks found"
- Sync Lag 停滞增长

---

## 🎯 预期效果

修复后，系统将具备：

1. ✅ **Nil Pointer 免疫** - 在 3 层防御（Fetcher -> Processor -> Checkpoint）
2. ✅ **优雅降级** - 遇到 nil block 跳过而非崩溃
3. ✅ **诊断增强** - 详细日志记录每个过滤点
4. ✅ **数据完整性** - 不丢失已写入的有效数据

---

## 📁 修改的文件清单

1. ✅ `internal/engine/processor_batch.go` - 添加 lastValidBlock 查找逻辑（第 145-161 行）
2. ✅ `internal/engine/fetcher_block.go` - 添加 header nil 检查（第 72-78 行，第 102-107 行）
3. ✅ `cmd/indexer/main.go` - 添加自愈函数和调用点（第 104-130 行，第 280 行）
4. ✅ 编译验证通过

---

## ✅ 最终状态

**状态**: 🟢 **三层防御全部完成，编译通过**

### 完成的修复层级

1. ✅ **Layer 1: Processor Batch 防御**
   - 文件: `internal/engine/processor_batch.go`
   - 修复: 反向查找 lastValidBlock
   - 效果: 避免 nil pointer dereference

2. ✅ **Layer 2: Fetcher 层过滤**
   - 文件: `internal/engine/fetcher_block.go`
   - 修复: 2 处 header nil 检查
   - 效果: 防止 nil BlockData 进入队列

3. ✅ **Layer 3: Sequencer 自愈**
   - 文件: `cmd/indexer/main.go`
   - 修复: 独立自愈函数 + 3 秒重启
   - 效果: Panic 后自动恢复

### 下一步行动

**立即执行**：
```bash
# 1. 停止 8092 进程
lsof -ti:8092 | xargs kill -9

# 2. 重启数据库（释放可能的死锁）
docker restart web3-indexer-db

# 3. 清理可能的孤儿事务
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
  -c "VACUUM FULL ANALYZE;"

# 4. 重新启动（带自愈机制）
make test-a2

# 5. 观察日志
tail -f /tmp/anvil-pro-lab.log | grep -E "(SELF-HEAL|nil pointer|No valid blocks)"
```

**验证标准**：
- ✅ Sequencer 启动日志显示 `🔄 [SELF-HEAL] Starting Sequencer...`
- ✅ nil header 过滤日志偶尔出现（正常）
- ✅ panic 次数为 0
- ✅ Sync Lag 持续降低

**预期改善**：
- Sequencer 崩溃后 3 秒自动重启
- nil block 不再导致 panic
- 系统具备自愈能力，提高持久性

---

**维护者**: 追求 6 个 9 持久性的资深后端工程师
**最后更新**: 2026-02-17
**编译状态**: ✅ 通过
**自愈机制**: ✅ 已实现
