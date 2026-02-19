package engine

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// IndexerState 索引器状态枚举
type IndexerState int32

const (
	StateIdle     IndexerState = iota // 休眠状态
	StateActive                       // 活跃演示状态
	StateWatching                     // 低成本监听状态
)

func (s IndexerState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateActive:
		return "active"
	case StateWatching:
		return "watching"
	default:
		return "unknown"
	}
}

// StateManager 智能状态管理器
type StateManager struct {
	// 组件引用
	indexer IndexerService
	rpcPool *RPCClientPool

	// 控制通道
	stateCh chan IndexerState
	stopCh  chan struct{}

	// 配置
	demoDuration   time.Duration // 演示模式持续时间
	idleTimeout    time.Duration // 闲置超时时间
	checkInterval  time.Duration // 检查间隔
	continuousMode bool          // 持续运行模式（禁用智能休眠）

	// 状态
	currentState atomic.Int32 // 当前状态
	lastAccess   atomic.Int64 // 最后访问时间(Unix纳秒)

	mu sync.RWMutex
}

// IndexerService 索引器服务接口
type IndexerService interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
	GetCurrentBlock() string
	SetLowPowerMode(enabled bool)
}

// NewStateManager 创建状态管理器
func NewStateManager(indexer IndexerService, rpcPool *RPCClientPool) *StateManager {
	// 检查是否启用持续运行模式（用于本地展示）
	continuousMode := os.Getenv("CONTINUOUS_MODE") == EnvTrue
	// 检查是否禁用智能睡眠系统（用于本地开发）
	disableSmartSleep := os.Getenv("DISABLE_SMART_SLEEP") == EnvTrue

	sm := &StateManager{
		indexer:        indexer,
		rpcPool:        rpcPool,
		stateCh:        make(chan IndexerState, 10),
		stopCh:         make(chan struct{}),
		demoDuration:   5 * time.Minute,  // 5分钟演示
		idleTimeout:    10 * time.Minute, // 10分钟无访问自动休眠
		checkInterval:  1 * time.Minute,  // 每分钟检查一次
		continuousMode: continuousMode,
	}

	// 初始状态
	if continuousMode || disableSmartSleep {
		// 持续模式或禁用智能睡眠时，直接启动为Active状态
		sm.currentState.Store(int32(StateActive))
		if continuousMode {
			Logger.Info("🚀 持续运行模式已开启，智能休眠已禁用")
		} else {
			Logger.Info("smart_sleep_disabled_starting_in_active_mode")
		}
	} else {
		sm.currentState.Store(int32(StateIdle))
	}

	sm.lastAccess.Store(time.Now().UnixNano())

	return sm
}

// Start 启动状态管理器
func (sm *StateManager) Start(ctx context.Context) {
	Logger.Info("state_manager_started",
		slog.String("initial_state", sm.GetState().String()),
		slog.Duration("demo_duration", sm.demoDuration),
		slog.Duration("idle_timeout", sm.idleTimeout),
		slog.Bool("continuous_mode", sm.continuousMode),
	)

	// 只有在非持续模式下才启动看门狗
	if !sm.continuousMode {
		go sm.watchdog(ctx)
	} else {
		Logger.Info("watchdog_disabled_in_continuous_mode")
	}

	// 启动状态处理器
	go sm.stateProcessor(ctx)
}
