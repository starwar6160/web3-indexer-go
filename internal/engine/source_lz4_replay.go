package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pierrec/lz4/v4"
)

// parseBigInt 从 JSON 值解析 *big.Int（支持数字和字符串）
func parseBigInt(val interface{}) *big.Int {
	if val == nil {
		return nil
	}

	var numStr string

	switch v := val.(type) {
	case float64: // JSON 数字默认解析为 float64
		numStr = fmt.Sprintf("%.0f", v)
	case string:
		numStr = v
	case int:
		numStr = fmt.Sprintf("%d", v)
	case int64:
		numStr = fmt.Sprintf("%d", v)
	case uint64:
		numStr = fmt.Sprintf("%d", v)
	default:
		return nil
	}

	result := new(big.Int)
	result, ok := result.SetString(numStr, 10)
	if !ok {
		return nil
	}

	return result
}

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

	// 🔥 FINDING-8 修复：使用 json.RawMessage 延迟 Data 字段解析
	// 避免 interface{} → json.Marshal → json.Unmarshal 的 GC 往返
	type replayEntry struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		var entry replayEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		if entry.Type == "block_data" {
			// 🔥 FINDING-8 修复：Data 已是 json.RawMessage，直接解析成 map
			var dataMap map[string]interface{}
			if err := json.Unmarshal(entry.Data, &dataMap); err != nil {
				continue
			}

			var bd BlockData

			// 解析 Number（可能是数字或字符串）
			if numVal, ok := dataMap["Number"]; ok {
				bd.Number = parseBigInt(numVal)
			}

			// 解析 RangeEnd（可能是数字或字符串）
			if rangeVal, ok := dataMap["RangeEnd"]; ok {
				bd.RangeEnd = parseBigInt(rangeVal)
			}

			// 🔥 FINDING-8 修复：直接从 RawMessage 解析，避免 Marshal 往返
			var tempStruct struct {
				Block interface{}            `json:"Block"`
				Err   map[string]interface{} `json:"Err"`
				Logs  []interface{}          `json:"Logs"`
			}

			if err := json.Unmarshal(entry.Data, &tempStruct); err != nil {
				continue
			}

			// 处理 Block 字段
			// 注意：录制时可能只保存了元数据，而不是完整的 Block
			if tempStruct.Block != nil {
				// 检查是否是完整的 Block 对象
				blockMap, ok := tempStruct.Block.(map[string]interface{})
				if ok && len(blockMap) == 2 {
					// 只有 ReceivedAt 和 ReceivedFrom，不是完整 Block
					// 忽略，保持 bd.Block = nil
					continue // 跳过不完整的 Block 元数据
				}
				// 尝试解析为完整 Block
				blockJSON, err := json.Marshal(tempStruct.Block)
				if err == nil {
					bd.Block = new(types.Block)
					if err := json.Unmarshal(blockJSON, bd.Block); err != nil {
						// Block 解析失败，设为 nil
						bd.Block = nil
					}
				}
			}

			// 处理 Err 字段（通常为 null）
			if tempStruct.Err == nil {
				bd.Err = nil
			}

			// 处理 Logs 字段
			if tempStruct.Logs != nil {
				logsJSON, err := json.Marshal(tempStruct.Logs)
				if err == nil {
					if err := json.Unmarshal(logsJSON, &bd.Logs); err != nil {
						bd.Logs = nil
					}
				}
			}

			bn := bd.Number.Uint64()
			if bn >= targetStart && bn <= targetEnd {
				// --- 🎬 倍速控制逻辑 ---
				// 注意：录制的 Block 可能为 nil（只包含元数据），需要检查
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
				// 只有当 Block 不为 nil 时才更新 lastTime
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

// StreamBlocks 推送模式：由 Source 驱动逐块推送到 channel
// 🔥 FINDING-6 修复：解决 Pull 模式下 scanner 前进导致稀疏数据漏失的问题
// 调用方只需从 channel 读取，不需要管理区块范围
func (s *Lz4ReplaySource) StreamBlocks(ctx context.Context, out chan<- BlockData) error {
	for s.scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := s.scanner.Bytes()
		var entry RecordEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "block_data" {
			continue
		}

		// 解析 BlockData
		dataMap, ok := entry.Data.(map[string]interface{})
		if !ok {
			continue
		}

		var bd BlockData
		if numVal, ok := dataMap["Number"]; ok {
			bd.Number = parseBigInt(numVal)
		}
		if bd.Number == nil {
			continue
		}
		if rangeVal, ok := dataMap["RangeEnd"]; ok {
			bd.RangeEnd = parseBigInt(rangeVal)
		}

		// 解析 Block、Logs 等字段
		tempJSON, err := json.Marshal(entry.Data)
		if err != nil {
			continue
		}
		var tempStruct struct {
			Block interface{}           `json:"Block"`
			Logs  []interface{}         `json:"Logs"`
		}
		if err := json.Unmarshal(tempJSON, &tempStruct); err != nil {
			continue
		}
		if tempStruct.Block != nil {
			blockMap, ok := tempStruct.Block.(map[string]interface{})
			if ok && len(blockMap) == 2 {
				continue // 不完整的 Block 元数据
			}
			blockJSON, err := json.Marshal(tempStruct.Block)
			if err == nil {
				bd.Block = new(types.Block)
				if err := json.Unmarshal(blockJSON, bd.Block); err != nil {
					bd.Block = nil
				}
			}
		}
		if tempStruct.Logs != nil {
			logsJSON, err := json.Marshal(tempStruct.Logs)
			if err == nil {
				if err := json.Unmarshal(logsJSON, &bd.Logs); err != nil {
					bd.Logs = nil
				}
			}
		}

		// 倍速控制
		if s.speedFactor > 0 && s.lastTime > 0 && bd.Block != nil {
			currentTime := bd.Block.Time()
			if currentTime > s.lastTime {
				diff := currentTime - s.lastTime
				sleepDur := time.Duration(float64(diff)/s.speedFactor) * time.Second
				if sleepDur > 2*time.Second {
					sleepDur = 200 * time.Millisecond
				}
				select {
				case <-time.After(sleepDur):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if bd.Block != nil {
			s.lastTime = bd.Block.Time()
		}

		s.lastNum = bd.Number.Uint64()

		// 推送到 channel
		select {
		case out <- bd:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return s.scanner.Err()
}
