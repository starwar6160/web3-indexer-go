package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"web3-indexer-go/internal/models"
)

// DataRecorder 负责将原始 RPC 数据录制到本地文件，以便后续回放
type DataRecorder struct {
	file *os.File
	mu   sync.Mutex
	path string
}

// NewDataRecorder 创建一个新的录制器
func NewDataRecorder(path string) (*DataRecorder, error) {
	if path == "" {
		// 默认存储在 logs 目录下，以时间戳命名
		timestamp := time.Now().Format("20060102_150405")
		path = fmt.Sprintf("logs/sepolia_capture_%s.jsonl", timestamp)
	}

	// #nosec G304 - Record files are stored in a safe local path
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}

	return &DataRecorder{
		file: f,
		path: path,
	}, nil
}

// RecordEntry 录制一条通用条目
type RecordEntry struct {
	Timestamp int64       `json:"ts"`
	Type      string      `json:"type"` // "block", "logs", "tx"
	Data      interface{} `json:"data"`
}

// Record 保存原始数据到磁盘
func (r *DataRecorder) Record(entryType string, data interface{}) {
	if r == nil || r.file == nil {
		return
	}

	entry := RecordEntry{
		Timestamp: time.Now().UnixMilli(),
		Type:      entryType,
		Data:      data,
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		log.Printf("⚠️ [Recorder] Failed to marshal entry: %v", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.file.Write(jsonData); err != nil {
		log.Printf("⚠️ [Recorder] Write failed: %v", err)
	}
	if _, err := r.file.WriteString("\n"); err != nil {
		log.Printf("⚠️ [Recorder] Write newline failed: %v", err)
	}
}

// DataSink Interface Implementation

func (r *DataRecorder) WriteTransfers(_ context.Context, transfers []models.Transfer) error {
	for _, t := range transfers {
		r.Record("transfer", t)
	}
	return nil
}

func (r *DataRecorder) WriteBlocks(_ context.Context, blocks []models.Block) error {
	for _, b := range blocks {
		r.Record("block", b)
	}
	return nil
}

// Close 关闭录制器
func (r *DataRecorder) Close() error {
	if r.file != nil {
		log.Printf("💾 [Recorder] Capture finished: %s", r.path)
		return r.file.Close()
	}
	return nil
}
