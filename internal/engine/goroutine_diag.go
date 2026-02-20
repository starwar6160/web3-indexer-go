package engine

import (
	"fmt"
	"log/slog"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"
)

// 🔥 Goroutine 死锁/阻塞诊断系统
// 用于在生产环境中快速定位协程阻塞点

type GoroutineDiagnostics struct {
	mu        sync.RWMutex
	snapshots []GoroutineSnapshot
	lastCheck time.Time
}

type GoroutineSnapshot struct {
	Timestamp   time.Time              `json:"timestamp"`
	Total       int                    `json:"total_goroutines"`
	ByState     map[string]int         `json:"by_state"`
	Blocked     []BlockedGoroutineInfo `json:"blocked,omitempty"`
	LongRunning []LongRunningGoroutine `json:"long_running,omitempty"`
}

type BlockedGoroutineInfo struct {
	ID       int64  `json:"id"`
	WaitTime string `json:"wait_time"`
	Stack    string `json:"stack_preview"`
}

type LongRunningGoroutine struct {
	ID       int64  `json:"id"`
	Duration string `json:"duration"`
	Stack    string `json:"stack_preview"`
}

var globalGoroutineDiagnostics = &GoroutineDiagnostics{
	snapshots: make([]GoroutineSnapshot, 0, 10),
}

// StartGoroutineMonitor 启动后台监控协程
func StartGoroutineMonitor(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			snapshot := captureGoroutineSnapshot()
			globalGoroutineDiagnostics.addSnapshot(snapshot)

			// 检测异常阻塞
			if blocked := detectBlockedGoroutines(snapshot); len(blocked) > 0 {
				slog.Warn("🔍 GOROUTINE_BLOCK_DETECTED",
					"blocked_count", len(blocked),
					"sample", blocked[0].Stack[:min(200, len(blocked[0].Stack))])
			}
		}
	}()
}

// captureGoroutineSnapshot 抓取当前所有协程状态
func captureGoroutineSnapshot() GoroutineSnapshot {
	buf := make([]byte, 1<<20) // 1MB buffer for stack traces
	n := runtime.Stack(buf, true)

	stackText := string(buf[:n])
	lines := strings.Split(stackText, "\n")

	snapshot := GoroutineSnapshot{
		Timestamp: time.Now(),
		ByState:   make(map[string]int),
	}

	var currentID int64
	var currentState string
	var stackBuilder strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// 协程记录结束
			if currentID != 0 {
				snapshot.Total++
				snapshot.ByState[currentState]++

				// 检测阻塞状态
				if strings.Contains(currentState, "chan send") ||
					strings.Contains(currentState, "chan receive") ||
					strings.Contains(currentState, "select") ||
					strings.Contains(currentState, "sync") {
					snapshot.Blocked = append(snapshot.Blocked, BlockedGoroutineInfo{
						ID:       currentID,
						WaitTime: "unknown",
						Stack:    stackBuilder.String(),
					})
				}
			}
			currentID = 0
			currentState = ""
			stackBuilder.Reset()
			continue
		}

		// 解析协程头部: "goroutine 42 [select]:"
		if strings.HasPrefix(line, "goroutine ") {
			// 手动解析，避免 fmt.Sscanf 不支持 %[^]] 字符集语法
			// 格式: goroutine <id> [<state>]:
			rest := line[len("goroutine "):]
			if spaceIdx := strings.IndexByte(rest, ' '); spaceIdx > 0 {
				idStr := rest[:spaceIdx]
				if _, err := fmt.Sscanf(idStr, "%d", &currentID); err != nil {
					currentID = 0
				}
				// 提取 [state] 部分
				if lbIdx := strings.IndexByte(rest, '['); lbIdx > 0 {
					if rbIdx := strings.IndexByte(rest, ']'); rbIdx > lbIdx {
						currentState = rest[lbIdx+1 : rbIdx]
					}
				}
			}
		}

		stackBuilder.WriteString(line)
		stackBuilder.WriteString("\n")
	}

	return snapshot
}

// detectBlockedGoroutines 检测长时间阻塞的协程
func detectBlockedGoroutines(snapshot GoroutineSnapshot) []BlockedGoroutineInfo {
	var critical []BlockedGoroutineInfo

	for _, blocked := range snapshot.Blocked {
		// 检测关键阻塞点
		if strings.Contains(blocked.Stack, "Processor") ||
			strings.Contains(blocked.Stack, "Sequencer") ||
			strings.Contains(blocked.Stack, "AsyncWriter") ||
			strings.Contains(blocked.Stack, "database") ||
			strings.Contains(blocked.Stack, "sql") {
			critical = append(critical, blocked)
		}
	}

	return critical
}

// GetLatestSnapshot 获取最新的诊断快照
func GetLatestSnapshot() GoroutineSnapshot {
	return globalGoroutineDiagnostics.getLatest()
}

// ExportFullGoroutineDump 导出完整堆栈（用于紧急调试）
func ExportFullGoroutineDump() string {
	buf := make([]byte, 4<<20) // 4MB
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// DumpGoroutinesToLog 将堆栈信息输出到日志
func DumpGoroutinesToLog() {
	dump := ExportFullGoroutineDump()
	slog.Warn("🔍 FULL_GOROUTINE_DUMP", "dump", dump)
}

// --- GoroutineDiagnostics 方法 ---

func (g *GoroutineDiagnostics) addSnapshot(s GoroutineSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.snapshots = append(g.snapshots, s)
	if len(g.snapshots) > 10 {
		g.snapshots = g.snapshots[1:]
	}
	g.lastCheck = time.Now()
}

func (g *GoroutineDiagnostics) getLatest() GoroutineSnapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.snapshots) == 0 {
		return GoroutineSnapshot{}
	}
	return g.snapshots[len(g.snapshots)-1]
}

// WriteHeapProfile 导出内存分析（配合 pprof 使用）
func WriteHeapProfile(w interface{ Write([]byte) (int, error) }) error {
	return pprof.WriteHeapProfile(w)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
