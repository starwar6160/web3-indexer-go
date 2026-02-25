package engine

import (
	"context"
	"sync"
	"time"
)

// MsgType 消息类型枚举
type MsgType int

const (
	CmdUpdateChainHeight  MsgType = iota // 发现新块高度
	CmdCommitBatch                       // 成功同步了一批交易 (逻辑完成)
	CmdCommitDisk                        // 成功落盘 (物理完成) - 🔥 横滨实验室 SSOT 关键
	CmdResetCursor                       // 强制重置游标 (用于 Reorg)
	CmdIncrementTransfers                // 增加转账计数
	CmdToggleEcoMode                     // 环境/配额触发休眠切换
	CmdSetSystemState                    // 设置系统状态
	CmdFetchFailed                       // 抓取失败 (用于调整安全缓冲)
	CmdFetchSuccess                      // 抓取成功 (用于重置失败计数)
	CmdNotifyFetched                     // 🚀 🔥 内存同步高度 (Fetcher 进度)
	CmdNotifyFetchProgress               // 🚀 🔥 新增：影子进度 (用于 UI 先行跳动)
	CmdLogEvent                          // 🚀 🔥 实时日志事件 (用于 UI 日志流)
	ReqGetStatus                         // UI 查询状态 (REQ/REP)
	ReqGetSnapshot                       // 获取状态快照 (REQ/REP)
)

// Message ZeroMQ 风格的消息结构
type Message struct {
	Type     MsgType
	Data     interface{}
	Reply    chan interface{} // 用于同步查询 (REQ/REP)
	Sequence uint64           // 全链路追踪 ID
}

// CoordinatorState 核心状态单例 (SSOT - Single Source of Truth)
// 所有状态的唯一真实来源,只有协调器能修改
type CoordinatorState struct {
	LatestHeight     uint64  // 链上最新高度
	TargetHeight     uint64  // 🎯 考虑安全垫后的目标高度
	FetchedHeight    uint64  // 🚀 🔥 新增：内存同步高度 (Fetcher 进度)
	SyncedCursor     uint64  // 数据库游标（已索引）
	Transfers        uint64  // 总转账数
	IsEcoMode        bool    // 是否处于休眠模式
	Progress         float64 // 同步进度百分比（统一计算，避免前端悖论）
	SystemState      SystemStateEnum
	UpdatedAt        time.Time // 状态更新时间
	LastUserActivity time.Time // 🔥 最后一次用户活动时间（用于休眠决策）
	SafetyBuffer     uint64    // 🚀 动态安全缓冲 (解决追尾 404)
	SuccessCount     uint64    // 🚀 🔥 新增：连续成功计数
	JobsDepth        int       // 🔥 任务队列深度
	ResultsDepth     int       // 🔥 结果队列深度
	LogEntry         map[string]interface{} // 🚀 🔥 新增：最新的日志条目
}

// Orchestrator 统一协调器（Actor 模型）
// 状态是私有的，只有协调器自己能改；外部只能通过发送"指令"来请求变更
type Orchestrator struct {
	cmdChan  chan Message     // 命令通道（深度缓冲应对 Anvil 高并发）
	state    CoordinatorState // 私有状态（仅协调器能改）
	mu       sync.RWMutex     // 仅用于对外提供快照读取
	snapshot CoordinatorState // 对外只读快照
	msgSeq   uint64           // 消息序列号生成器
	ctx      context.Context
	cancel   context.CancelFunc

	// 🔥 横滨实验室：环境感知配置
	isYokohamaLab bool // Anvil 环境 (128G RAM)

	// 🔥 订阅者管理（用于 WS 广播）
	broadcastCh chan CoordinatorState
	subscribers []chan CoordinatorState

	// 🔥 结构化日志配置
	enableProfiling bool

	// 🔥 消息合并策略（防止 Channel 溢出）
	pendingHeightUpdate *uint64 // 待合并的高度更新
	lastHeightMergeTime time.Time

	// 🔥 异步持久化流水线
	asyncWriter *AsyncWriter // 异步写入器引用

	// 🔥 组件引用 (用于监控)
	fetcher  *Fetcher
	strategy EngineStrategy // 🚀 🔥 新增：运行策略 (Anvil vs Testnet)
}
