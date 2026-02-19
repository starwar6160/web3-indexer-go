package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// ThrottledHub 带节流的 WebSocket Hub
// 用于横滨实验室高频环境下的指标聚合推送
type ThrottledHub struct {
	*Hub

	// 🔥 节流配置
	throttleInterval time.Duration // 节流间隔（默认 500ms）
	aggregateEvents  []interface{} // 聚合的事件缓冲区
	aggregateMu      sync.Mutex    // 聚合缓冲区锁
	lastBroadcast    time.Time     // 上次广播时间
	ticker           *time.Ticker  // 定时广播触发器

	// 统计
	totalEvents       uint64
	droppedEvents     uint64
	aggregatedBatches uint64
}

// NewThrottledHub 创建带节流的 Hub
func NewThrottledHub(throttleInterval time.Duration) *ThrottledHub {
	// 🚀 横滨实验室：默认使用 200ms (5 FPS) 以获得最佳视觉节奏感
	if throttleInterval > 200*time.Millisecond {
		throttleInterval = 200 * time.Millisecond
	}
	baseHub := NewHub()
	return &ThrottledHub{
		Hub:              baseHub,
		throttleInterval: throttleInterval,
		aggregateEvents:  make([]interface{}, 0, 1000), // 预分配 1000 容量
		lastBroadcast:    time.Now(),
	}
}

// RunWithThrottling 启动带节流的 Hub
func (h *ThrottledHub) RunWithThrottling(ctx context.Context) {
	h.logger.Info("🔥 Throttled WebSocket Hub started",
		"throttle_interval", h.throttleInterval,
		"buffer_size", cap(h.aggregateEvents))

	// 启动节流广播协程
	h.ticker = time.NewTicker(h.throttleInterval)
	defer h.ticker.Stop()

	// 节流广播协程
	go h.throttledBroadcaster(ctx)

	// 运行基础 Hub 逻辑
	h.Hub.Run(ctx)
}

// throttledBroadcaster 定期聚合广播
func (h *ThrottledHub) throttledBroadcaster(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.ticker.C:
			h.flushAggregatedEvents()
		}
	}
}

// BroadcastWithThrottle 带节流的广播（聚合高频事件）
func (h *ThrottledHub) BroadcastWithThrottle(event interface{}) {
	h.totalEvents++

	// 🔥 关键事件类型立即推送（不节流）
	eventType := getEventType(event)
	if shouldImmediateBroadcast(eventType) {
		h.Hub.Broadcast(event)
		return
	}

	// 其他事件类型聚合推送
	h.aggregateMu.Lock()
	defer h.aggregateMu.Unlock()

	h.aggregateEvents = append(h.aggregateEvents, event)

	// 如果缓冲区快满了，立即触发广播（防止内存溢出）
	if len(h.aggregateEvents) >= cap(h.aggregateEvents) {
		h.logger.Warn("🔥 ThrottledHub buffer full, flushing immediately",
			"buffer_size", len(h.aggregateEvents))
		h.aggregateMu.Unlock()
		h.flushAggregatedEvents()
		h.aggregateMu.Lock()
	}
}

// flushAggregatedEvents 刷新聚合事件到广播
func (h *ThrottledHub) flushAggregatedEvents() {
	h.aggregateMu.Lock()
	defer h.aggregateMu.Unlock()

	if len(h.aggregateEvents) == 0 {
		return
	}

	// 🔥 智能聚合：只保留最新状态，丢弃中间过渡状态
	aggregated := h.smartAggregate(h.aggregateEvents)

	h.aggregatedBatches++
	h.logger.Debug("📊 ThrottledHub flushing aggregated events",
		"total_events", len(h.aggregateEvents),
		"aggregated_to", len(aggregated),
		"total_batches", h.aggregatedBatches)

	// 批量广播
	for _, event := range aggregated {
		message, err := json.Marshal(event)
		if err != nil {
			h.logger.Error("ws_json_marshal_error", slog.String("error", err.Error()))
			continue
		}

		// 使用基础 Hub 的广播逻辑（避免重复代码）
		for client := range h.Hub.clients {
			select {
			case client.send <- message:
			default:
				// 客户端阻塞，丢弃
				h.droppedEvents++
			}
		}
	}

	// 清空缓冲区
	h.aggregateEvents = h.aggregateEvents[:0]
	h.lastBroadcast = time.Now()
}

// smartAggregate 智能聚合：只保留最新状态
func (h *ThrottledHub) smartAggregate(events []interface{}) []interface{} {
	// 按事件类型分组，每种类型只保留最新的一个
	typeLatest := make(map[string]interface{})

	for _, event := range events {
		eventType := getEventType(event)
		// 只保留最新的事件（覆盖旧的）
		typeLatest[eventType] = event
	}

	// 转换回切片
	result := make([]interface{}, 0, len(typeLatest))
	for _, event := range typeLatest {
		result = append(result, event)
	}

	return result
}

// getEventType 获取事件类型
func getEventType(event interface{}) string {
	if wsEvent, ok := event.(WSEvent); ok {
		return wsEvent.Type
	}
	return "unknown"
}

// shouldImmediateBroadcast 判断是否应该立即广播
func shouldImmediateBroadcast(eventType string) bool {
	// 🔥 关键事件立即推送
	immediateTypes := map[string]bool{
		"system_healing":   true, // 自愈事件
		"engine_panic":     true, // 崩溃事件
		"linearity_status": true, // 线性检查状态
		"lazy_status":      true, // LazyManager 状态变化
	}

	return immediateTypes[eventType]
}

// GetStats 获取节流统计（用于监控）
func (h *ThrottledHub) GetStats() map[string]interface{} {
	h.aggregateMu.Lock()
	defer h.aggregateMu.Unlock()

	return map[string]interface{}{
		"total_events":       h.totalEvents,
		"dropped_events":     h.droppedEvents,
		"aggregated_batches": h.aggregatedBatches,
		"pending_events":     len(h.aggregateEvents),
		"buffer_capacity":    cap(h.aggregateEvents),
		"throttle_interval":  h.throttleInterval.String(),
		"last_broadcast":     h.lastBroadcast.Format(time.RFC3339),
	}
}
