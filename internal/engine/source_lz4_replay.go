package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/pierrec/lz4/v4"
)

// Lz4ReplaySource LZ4 轨迹回放源
// 实现了 BlockSource 接口，将压缩文件伪装成实时区块链
type Lz4ReplaySource struct {
	file        *os.File
	lz4Reader   *lz4.Reader
	scanner     *bufio.Scanner
	path        string
	totalSize   int64
	lastNum     uint64
	lastTime    uint64  // 链上最后一个区块的时间戳
	speedFactor float64 // 0: 全速, 1: 真实速度, 10: 十倍速
}

// NewLz4ReplaySource 创建回放源
func NewLz4ReplaySource(path string, speed float64) (*Lz4ReplaySource, error) {
	// #nosec G304 - path is from controlled configuration
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	zr := lz4.NewReader(f)
	scanner := bufio.NewScanner(zr)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	return &Lz4ReplaySource{
		file:        f,
		lz4Reader:   zr,
		scanner:     scanner,
		path:        path,
		totalSize:   fi.Size(),
		speedFactor: speed,
	}, nil
}

// GetProgress 返回当前回放进度百分比
func (s *Lz4ReplaySource) GetProgress() float64 {
	if s.totalSize == 0 {
		return 0
	}
	// 通过底层文件指针位置估算压缩流进度
	pos, err := s.file.Seek(0, 1) // io.SeekCurrent
	if err != nil {
		return 0
	}
	return float64(pos) / float64(s.totalSize) * 100
}

// FetchLogs 从 LZ4 轨迹中提取区块数据，并执行倍速休眠
func (s *Lz4ReplaySource) FetchLogs(ctx context.Context, start, end *big.Int) ([]BlockData, error) {
	var results []BlockData
	targetStart := start.Uint64()
	targetEnd := end.Uint64()

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		var entry RecordEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		if entry.Type == "block_data" {
			// ⚡ 工业级黑科技：利用 RecordEntry 的 Timestamp 或 Data 里的 Block 时间进行节拍控制
			var bd BlockData
			tempJSON, err := json.Marshal(entry.Data)
			if err != nil {
				continue
			}
			if err := json.Unmarshal(tempJSON, &bd); err != nil {
				continue
			}

			bn := bd.Number.Uint64()
			if bn >= targetStart && bn <= targetEnd {
				// --- 🎬 倍速控制逻辑 ---
				if s.speedFactor > 0 && s.lastTime > 0 && bd.Block != nil {
					currentTime := bd.Block.Time()
					if currentTime > s.lastTime {
						diff := currentTime - s.lastTime
						sleepDur := time.Duration(float64(diff)/s.speedFactor) * time.Second

						// 物理保护：单次休眠不超过 2s，防止回放卡死
						if sleepDur > 2*time.Second {
							sleepDur = 200 * time.Millisecond
						}

						select {
						case <-time.After(sleepDur):
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
				}
				if bd.Block != nil {
					s.lastTime = bd.Block.Time()
				}

				results = append(results, bd)
				s.lastNum = bn
			}

			if bn >= targetEnd {
				break
			}
		}
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("lz4_scan_failed: %w", err)
	}

	return results, nil
}

// GetLatestHeight 返回文件中已知的最高块，或者一个极大值以维持运行
func (s *Lz4ReplaySource) GetLatestHeight(_ context.Context) (*big.Int, error) {
	// 在回放模式下，我们通常让引擎一直跑直到 EOF
	return big.NewInt(999999999), nil
}

func (s *Lz4ReplaySource) Close() error {
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// Reset 重置回放，回到文件开头
func (s *Lz4ReplaySource) Reset() error {
	_, err := s.file.Seek(0, 0)
	if err != nil {
		return err
	}
	s.lz4Reader.Reset(s.file)
	s.scanner = bufio.NewScanner(s.lz4Reader)
	buf := make([]byte, 0, 1024*1024)
	s.scanner.Buffer(buf, 10*1024*1024)
	return nil
}
