package engine

import (
	"context"
	"math/big"
	"testing"
	"time"
)

// TestStage4_GapBypass_VisualConsistency 验证空洞跳过后 UI 数字依然能对齐
func TestStage4_GapBypass_VisualConsistency(t *testing.T) {
	// 创建模拟处理器
	mockProcessor := &MockBlockProcessor{}

	// 创建 Sequencer，从块 100 开始
	startBlock := big.NewInt(100)
	resultCh := make(chan BlockData, 100)
	fatalErrCh := make(chan error, 1)

	seq := NewSequencerWithFetcher(
		mockProcessor,
		nil, // 不使用 fetcher
		startBlock,
		11155111,
		resultCh,
		fatalErrCh,
		nil,
		nil,
	)

	// 构造空洞场景：有 100, 102，缺 101
	resultCh <- BlockData{Number: big.NewInt(100)}
	resultCh <- BlockData{Number: big.NewInt(102)} // 空洞：缺少 101

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// 启动 Sequencer（后台）
	go seq.Run(ctx)

	// 等待空洞检测
	time.Sleep(70 * time.Second) // 超过 60 秒阈值

	// 验证：游标应该跳到 102（跳过了空洞的 101）
	seq.mu.RLock()
	expected := seq.expectedBlock
	seq.mu.RUnlock()

	if expected.Int64() < 102 {
		t.Fatalf("AI_FIX_REQUIRED [Stage 4]: Gap bypass failed. Cursor stuck at %d, expected >= 102", expected.Int64())
	}

	t.Logf("✅ SUCCESS: Pipeline unblocked by bypass logic. expectedBlock=%d", expected.Int64())
}

// TestStage4_Persistence_Encoding_AI_Friendly 验证 SQL 类型编码
func TestStage4_Persistence_Encoding_AI_Friendly(t *testing.T) {
	// 这个测试验证 AsyncWriter 的 SQL 编码不会报错
	// 实际集成测试需要真实的数据库连接
	t.Skip("Requires database connection - run with integration test suite")
}

// MockBlockProcessor 模拟处理器
type MockBlockProcessor struct{}

func (m *MockBlockProcessor) ProcessBlockWithRetry(_ context.Context, data BlockData, maxRetries int) error {
	_ = data // 避免未使用警告
	_ = maxRetries
	return nil
}

func (m *MockBlockProcessor) ProcessBatch(ctx context.Context, blocks []BlockData, chainID int64) error {
	return nil
}

func (m *MockBlockProcessor) GetRPCClient() RPCClient {
	return nil
}
