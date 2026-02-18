package engine

import (
	"context"
	"sync"
	"web3-indexer-go/internal/models"
)

// HotBuffer 极致压榨内存的热数据池
// 专为 9GB 可用内存环境设计，确保 API 零延迟响应
type HotBuffer struct {
	mu        sync.RWMutex
	transfers []models.Transfer
	maxSize   int
}

// NewHotBuffer 创建热数据池
func NewHotBuffer(maxSize int) *HotBuffer {
	if maxSize <= 0 {
		maxSize = 50000 // 默认 5 万条，约占 15MB 内存
	}
	return &HotBuffer{
		transfers: make([]models.Transfer, 0, maxSize),
		maxSize:   maxSize,
	}
}

// Add 添加新的转账记录到内存池
func (b *HotBuffer) Add(t models.Transfer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.addLocked(t)
}

func (b *HotBuffer) addLocked(t models.Transfer) {
	b.transfers = append(b.transfers, t)
	if len(b.transfers) > b.maxSize {
		drain := b.maxSize / 10
		b.transfers = append([]models.Transfer(nil), b.transfers[drain:]...)
	}
}

// DataSink Interface Implementation

func (b *HotBuffer) WriteTransfers(_ context.Context, transfers []models.Transfer) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range transfers {
		b.addLocked(t)
	}
	return nil
}

func (b *HotBuffer) WriteBlocks(_ context.Context, _ []models.Block) error {
	// 内存池目前仅关注转账热数据，忽略区块元数据以节省空间
	return nil
}

func (b *HotBuffer) Close() error { return nil }

// GetLatest 获取最新的 N 条转账记录
func (b *HotBuffer) GetLatest(limit int) []models.Transfer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	n := len(b.transfers)
	if n == 0 {
		return nil
	}

	if limit > n {
		limit = n
	}

	// 返回切片副本，防止外部修改影响原始数据
	result := make([]models.Transfer, limit)
	copy(result, b.transfers[n-limit:])

	// 反转顺序：让最新的排在前面
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// GetCount 获取当前缓存数量
func (b *HotBuffer) GetCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.transfers)
}
