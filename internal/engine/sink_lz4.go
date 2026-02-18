package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"web3-indexer-go/internal/models"

	"github.com/pierrec/lz4/v4"
)

// Lz4Sink 高性能压缩归宿
// 专为 5600U 设计，利用冗余算力换取存储空间，减少 SSD 损耗
type Lz4Sink struct {
	file      *os.File
	lz4Writer *lz4.Writer
	mu        sync.Mutex
	path      string
	suspended bool // 🚀 空间不足时自动挂起
}

func NewLz4Sink(path string) (*Lz4Sink, error) {
	// #nosec G304 - 录制路径由系统配置控制
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	// 初始化 klauspost 优化版的 LZ4 Writer
	// 默认配置已足够快，适合 5600U 的多核异构架构
	zw := lz4.NewWriter(f)

	return &Lz4Sink{
		file:      f,
		lz4Writer: zw,
		path:      path,
	}, nil
}

func (s *Lz4Sink) checkQuota() bool {
	if s.suspended {
		return false
	}

	// 检查工作目录的剩余空间
	free, err := CheckStorageSpace(".")
	if err != nil {
		return true // 如果获取失败，出于安全考虑假设空间足够，但在 Write 中会有错误处理
	}

	if free < 10.0 {
		s.suspended = true
		Logger.Error("🚨 STORAGE_QUOTA_EXCEEDED", "free_percent", fmt.Sprintf("%.2f%%", free), "action", "suspending_recording")
		return false
	}
	return true
}

func (s *Lz4Sink) WriteTransfers(_ context.Context, transfers []models.Transfer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.checkQuota() {
		return nil // 物理挂起，不报错以防止中断主流程
	}

	for _, t := range transfers {
		data, err := json.Marshal(t)
		if err != nil {
			continue
		}
		if _, err := s.lz4Writer.Write(data); err != nil {
			return err
		}
		if _, err := s.lz4Writer.Write([]byte("\n")); err != nil {
			return err
		}
	}

	// ⚡ 实时刷新到 LZ4 缓冲区
	return s.lz4Writer.Flush()
}

func (s *Lz4Sink) WriteBlocks(_ context.Context, blocks []models.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.checkQuota() {
		return nil
	}

	for _, b := range blocks {
		data, err := json.Marshal(b)
		if err != nil {
			continue
		}
		if _, err := s.lz4Writer.Write(data); err != nil {
			return err
		}
		if _, err := s.lz4Writer.Write([]byte("\n")); err != nil {
			return err
		}
	}

	return s.lz4Writer.Flush()
}

func (s *Lz4Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lz4Writer != nil {
		_ = s.lz4Writer.Close()
	}
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}
