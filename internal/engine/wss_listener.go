package engine

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	mrand "math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// WSSListener 通过 WebSocket 实时监听新块
type WSSListener struct {
	newBlocks chan *big.Int
	stopCh    chan struct{}
	stopOnce  sync.Once
	mu        sync.RWMutex
	wssURL    string
	client    *ethclient.Client
	connected bool

	// 重连状态管理
	reconnectCount int           // 当前重连次数
	maxReconnects  int           // 最大重连次数（0=无限）
	baseBackoff    time.Duration // 基础退避时间（1s）
	maxBackoff     time.Duration // 最大退避时间（60s）
}

// NewWSSListener 创建 WSS 监听器
func NewWSSListener(wssURL string) (*WSSListener, error) {
	if wssURL == "" {
		return nil, fmt.Errorf("WSS URL is required")
	}

	client, err := ethclient.Dial(wssURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to WSS: %w", err)
	}

	return &WSSListener{
		wssURL:        wssURL,
		client:        client,
		newBlocks:     make(chan *big.Int, 10),
		stopCh:        make(chan struct{}),
		connected:     true,
		maxReconnects: 0, // 默认无限重试
		baseBackoff:   1 * time.Second,
		maxBackoff:    60 * time.Second,
	}, nil
}

// Start 启动 WSS 监听
func (w *WSSListener) Start(ctx context.Context) {
	go w.listenNewHeads(ctx)
}

// calculateBackoff 计算指数退避时间
func (w *WSSListener) calculateBackoff() time.Duration {
	// 指数退避：1s, 2s, 4s, 8s, 16s, 32s, max 60s
	exponentialBackoff := float64(w.baseBackoff) * math.Pow(2, float64(w.reconnectCount))
	if exponentialBackoff > float64(w.maxBackoff) {
		exponentialBackoff = float64(w.maxBackoff)
	}

	// 添加抖动 ±25%（防止惊群效应）
	jitter := 1.0 + (mrand.Float64()*0.5 - 0.25)
	backoff := time.Duration(exponentialBackoff * jitter)

	return backoff
}

// listenNewHeads 监听新块头（带指数退避重连）
func (w *WSSListener) listenNewHeads(ctx context.Context) {
	for {
		// 检查是否超过最大重连次数
		if w.maxReconnects > 0 && w.reconnectCount >= w.maxReconnects {
			log.Printf("❌ WSS max reconnections (%d) exceeded, giving up", w.maxReconnects)
			w.setConnected(false)
			return
		}

		headers := make(chan *types.Header)
		sub, err := w.client.SubscribeNewHead(ctx, headers)
		if err != nil {
			log.Printf("❌ WSS subscription failed: %v", err)
			w.handleReconnect(ctx)
			continue
		}

		log.Printf("✅ WSS listener connected to %s (attempt %d)", w.wssURL, w.reconnectCount+1)
		w.setConnected(true)
		w.reconnectCount = 0 // 成功连接后重置计数器

		for {
			select {
			case <-ctx.Done():
				sub.Unsubscribe()
				return
			case <-w.stopCh:
				sub.Unsubscribe()
				return
			case header := <-headers:
				if header != nil {
					select {
					case w.newBlocks <- header.Number:
						log.Printf("📦 New block detected via WSS: %s", header.Number.String())
					case <-w.stopCh:
						sub.Unsubscribe()
						return
					}
				}
			case err := <-sub.Err():
				log.Printf("⚠️ WSS subscription error: %v", err)
				w.setConnected(false)
				// 退出内层循环，触发重连
				sub.Unsubscribe()
				goto reconnect
			}
		}

	reconnect:
		// 计算退避时间
		backoff := w.calculateBackoff()
		log.Printf("🔄 Reconnecting to WSS in %v (attempt %d)", backoff, w.reconnectCount+1)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			w.reconnectCount++
			// 继续外层循环进行重连
		}
	}
}

// handleReconnect 处理重连前的准备
func (w *WSSListener) handleReconnect(ctx context.Context) {
	backoff := w.calculateBackoff()
	log.Printf("🔄 Reconnecting to WSS in %v (attempt %d)", backoff, w.reconnectCount+1)

	select {
	case <-ctx.Done():
		return
	case <-time.After(backoff):
		w.reconnectCount++
	}
}

// GetNewBlocks 获取新块通道
func (w *WSSListener) GetNewBlocks() <-chan *big.Int {
	return w.newBlocks
}

// IsConnected 检查是否连接
func (w *WSSListener) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connected
}

// setConnected 设置连接状态
func (w *WSSListener) setConnected(connected bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connected = connected
}

// Stop 停止监听
func (w *WSSListener) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		if w.client != nil {
			w.client.Close()
		}
	})
}
