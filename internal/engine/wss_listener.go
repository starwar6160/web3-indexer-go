package engine

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// WSSListener 通过 WebSocket 实时监听新块
type WSSListener struct {
	wssURL    string
	client    *ethclient.Client
	newBlocks chan *big.Int
	stopCh    chan struct{}
	stopOnce  sync.Once
	mu        sync.RWMutex
	connected bool
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
		wssURL:    wssURL,
		client:    client,
		newBlocks: make(chan *big.Int, 10),
		stopCh:    make(chan struct{}),
		connected: true,
	}, nil
}

// Start 启动 WSS 监听
func (w *WSSListener) Start(ctx context.Context) {
	go w.listenNewHeads(ctx)
}

// listenNewHeads 监听新块头
func (w *WSSListener) listenNewHeads(ctx context.Context) {
	headers := make(chan *types.Header)
	sub, err := w.client.SubscribeNewHead(ctx, headers)
	if err != nil {
		log.Printf("❌ WSS subscription failed: %v", err)
		w.setConnected(false)
		return
	}
	defer sub.Unsubscribe()

	log.Printf("✅ WSS listener connected to %s", w.wssURL)
	w.setConnected(true)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case header := <-headers:
			if header != nil {
				select {
				case w.newBlocks <- header.Number:
					log.Printf("📦 New block detected via WSS: %s", header.Number.String())
				case <-w.stopCh:
					return
				}
			}
		case err := <-sub.Err():
			log.Printf("⚠️ WSS subscription error: %v", err)
			w.setConnected(false)
			// 尝试重新连接
			time.Sleep(5 * time.Second)
			go w.listenNewHeads(ctx)
			return
		}
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
