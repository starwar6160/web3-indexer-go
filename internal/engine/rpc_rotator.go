package engine

import (
	"log/slog"
	"sync/atomic"
)

// RPCRotator 多源 RPC 负载均衡器
// 设计理念：自动在不同 Provider 之间切换，防止单点故障
type RPCRotator struct {
	nodes      []*RPCNode
	currentIdx uint32
}

// RPCNode RPC 节点定义
type RPCNode struct {
	URL        string
	Weight     int   // 权重（用于加权轮询）
	IsHealthy  bool  // 健康状态
	ErrorCount int64 // 错误计数
}

// NewRPCRotator 创建 RPC 轮询器
func NewRPCRotator(urls []string) *RPCRotator {
	nodes := make([]*RPCNode, len(urls))
	for i, url := range urls {
		nodes[i] = &RPCNode{
			URL:       url,
			Weight:    1, // 默认权重
			IsHealthy: true,
		}
	}
	return &RPCRotator{
		nodes: nodes,
	}
}

// GetNext 获取下一个可用的 RPC 节点（Round Robin）
func (r *RPCRotator) GetNext() string {
	if len(r.nodes) == 0 {
		return ""
	}
	// #nosec G115 - len(r.nodes) is checked above and realistically small
	n := uint32(len(r.nodes))
	for i := 0; i < len(r.nodes); i++ {
		idx := atomic.AddUint32(&r.currentIdx, 1) % n
		node := r.nodes[idx]
		if node.IsHealthy {
			return node.URL
		}
	}
	// 如果全部挂了，返回第一个（触发报错）
	return r.nodes[0].URL
}

// MarkUnhealthy 标记节点为不健康
func (r *RPCRotator) MarkUnhealthy(url string) {
	for _, node := range r.nodes {
		if node.URL == url {
			node.IsHealthy = false
			slog.Warn("🚨 RPC_NODE_UNHEALTHY", "url", maskURL(url))
			return
		}
	}
}

// MarkHealthy 标记节点为健康
func (r *RPCRotator) MarkHealthy(url string) {
	for _, node := range r.nodes {
		if node.URL == url {
			node.IsHealthy = true
			slog.Info("✅ RPC_NODE_RECOVERED", "url", maskURL(url))
			return
		}
	}
}

// RecordError 记录节点错误
func (r *RPCRotator) RecordError(url string) {
	for _, node := range r.nodes {
		if node.URL == url {
			count := atomic.AddInt64(&node.ErrorCount, 1)
			// 连续错误超过 10 次，标记为不健康
			if count > 10 {
				r.MarkUnhealthy(url)
			}
			return
		}
	}
}

// GetHealthyCount 获取健康节点数量
func (r *RPCRotator) GetHealthyCount() int {
	count := 0
	for _, node := range r.nodes {
		if node.IsHealthy {
			count++
		}
	}
	return count
}

// maskURL 掩码 URL（保护密钥）
func maskURL(url string) string {
	if len(url) > 20 {
		return url[:10] + "..." + url[len(url)-10:]
	}
	return url
}
