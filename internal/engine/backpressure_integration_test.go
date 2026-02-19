//go:build integration

package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/stretchr/testify/assert"
)

// TestIntegration_BackpressureFlow 验证当磁盘写入无法跟上内存抓取时，系统是否能正确识别压力
func TestIntegration_BackpressureFlow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	orchestrator := GetOrchestrator()
	orchestrator.Reset()

	writer := NewAsyncWriter(db, orchestrator, false)
	orchestrator.SetAsyncWriter(writer)

	// 🚀 模拟生产者：填满 AsyncWriter 的队列
	capacity := cap(writer.taskChan)
	fillCount := capacity * 85 / 100

	t.Logf("🚀 Filling task channel with %d tasks to trigger pressure limit", fillCount)
	for i := 1; i <= fillCount; i++ {
		writer.taskChan <- PersistTask{
			Height: uint64(i), // #nosec G115 - i is small and positive
			Block: models.Block{
				Number: models.NewBigInt(int64(i)),
				Hash:   fmt.Sprintf("0x%d", i),
			},
		}
	}

	// 🚀 模拟背压感知：手动同步深度到 GlobalState (模拟 evaluateSystemState 的动作)
	GetGlobalState().UpdatePipelineDepth(0, int32(fillCount), 0) // #nosec G115 - fillCount is bounded by channel capacity

	// 验证状态
	status := orchestrator.GetUIStatus(context.Background(), db, "test-v1")
	assert.Equal(t, "pressure_limit", status.State, "系统应识别到 I/O 瓶颈并进入限流状态")
	assert.Equal(t, fillCount, status.ResultsDepth, "任务队列应有正确积压")
}

// TestIntegration_WatermarkLogic 验证影子同步高度与物理高度的单调性约束
func TestIntegration_WatermarkLogic(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	orchestrator := GetOrchestrator()
	orchestrator.Reset()
	writer := NewAsyncWriter(db, orchestrator, false)
	orchestrator.SetAsyncWriter(writer)
	writer.Start() // 启动写入器
	defer func() {
		err := writer.Shutdown(1 * time.Second)
		assert.NoError(t, err)
	}()

	// 模拟连续数据流
	for i := uint64(1); i <= 50; i++ {
		// 1. 模拟抓取完成 (MemorySync)
		orchestrator.Dispatch(CmdNotifyFetched, i)

		// 2. 模拟逻辑处理完成并提交落盘任务
		task := PersistTask{
			Height: i,
			Block: models.Block{
				Number: models.NewBigInt(int64(i)),
				Hash:   fmt.Sprintf("0x%d", i),
			},
		}
		orchestrator.Dispatch(CmdCommitBatch, task)

		// 验证快照：在任何时刻，FetchedHeight >= SyncedCursor
		// 由于异步性，我们给一点点处理时间
		time.Sleep(5 * time.Millisecond)
		snap := orchestrator.GetSnapshot()
		assert.GreaterOrEqual(t, snap.FetchedHeight, snap.SyncedCursor, "逻辑高度必须领先或等于物理高度")
	}
}

// TestIntegration_ReliefValve 验证紧急泄压阀的‘丢卒保车’逻辑
func TestIntegration_ReliefValve(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	orchestrator := GetOrchestrator()
	orchestrator.Reset()
	writer := NewAsyncWriter(db, orchestrator, false)

	capacity := cap(writer.taskChan)
	fillCount := capacity * 95 / 100 // 填充 95%

	t.Logf("🚀 Filling channel with %d tasks to trigger relief valve (capacity: %d)", fillCount, capacity)

	for i := 1; i <= fillCount; i++ {
		height := uint64(i) // #nosec G115 - i is small and positive
		writer.taskChan <- PersistTask{
			Height: height,
			Block: models.Block{
				Number: models.NewBigInt(int64(height)), // #nosec G115 - height is small and positive
				Hash:   fmt.Sprintf("0x%d", height),
			},
		}
	}

	// 触发泄压逻辑
	writer.emergencyDrain()

	// 🚀 给一丁点时间让 Orchestrator 内部状态更新
	time.Sleep(10 * time.Millisecond)

	// 验证结果
	currentDepth := len(writer.taskChan)
	targetDepth := capacity * 50 / 100
	assert.LessOrEqual(t, currentDepth, targetDepth+1, "泄压阀应将深度降至 50% 附近")

	snap := orchestrator.GetSnapshot()
	// lastHeight 是在循环中记录的最后一个被弹出的高度
	// 由于我们填充了 1..fillCount，弹出了 (fillCount - targetDepth) 个元素
	// 所以最后一个被弹出元素的高度应该是 (fillCount - targetDepth)
	expectedHeight := uint64(fillCount - currentDepth) // #nosec G115 - result is non-negative and small
	assert.GreaterOrEqual(t, snap.SyncedCursor, expectedHeight, "游标应跳跃至最后丢弃的高度")
}
