package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/gorilla/websocket"
)

// WSEvent 定义发送到前端的消息结构
type WSEvent struct {
	Data interface{} `json:"data"`
	Type string      `json:"type"` // "block" or "transfer"
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 30 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 🚀 工业级安全保护：限制跨域请求，防止 CSRF/WebSocket Hijacking
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // 允许非浏览器请求
		}

		// 允许本地开发环境 (localhost/127.0.0.1 和 mp1 域名)
		if strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1") || strings.Contains(origin, "mp1") {
			return true
		}

		// 允许指定的演示域名 (横滨实验室官方域名)
		if strings.Contains(origin, "st6160.click") {
			return true
		}

		slog.Warn("🚫 [Security] Blocked unauthorized WebSocket origin", "origin", origin)
		return false
	},
}

// Client 代表一个连接的前端用户
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub 负责维护活跃连接和广播消息
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan interface{}
	register   chan *Client
	unregister chan *Client
	logger     *slog.Logger
	OnActivity func()            // 🚀 Activity callback for On-Demand logic
	OnNeedMeta func(addr string) // 🎨 Metadata request callback
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan interface{}, 1024), // 增加缓冲区防止丢消息
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		logger:     engine.Logger,
	}
}

func (h *Hub) Run(ctx context.Context) {
	h.logger.Info("websocket_hub_started")
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("websocket_hub_stopping")
			// 优雅关闭：关闭所有客户端连接
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			return
		case client := <-h.register:
			h.clients[client] = true
			if h.OnActivity != nil {
				h.OnActivity() // WebSocket connection is an activity
			}
			h.logger.Info("ws_client_connected", slog.Int("total_clients", len(h.clients)))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.logger.Info("ws_client_disconnected", slog.Int("total_clients", len(h.clients)))
			}

		case event := <-h.broadcast:
			// 序列化消息
			message, err := json.Marshal(event)
			if err != nil {
				h.logger.Error("ws_json_marshal_error", slog.String("error", err.Error()))
				continue
			}

			// 广播给所有客户端
			if len(h.clients) == 0 {
				continue
			}

			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					h.logger.Warn("ws_client_blocked_dropping_client")
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Broadcast 对外暴露的广播方法，非阻塞
func (h *Hub) Broadcast(event interface{}) {
	select {
	case h.broadcast <- event:
	default:
		// 如果 Hub 处理不过来，丢弃消息，保证 Indexer 核心不卡死
		h.logger.Warn("ws_hub_blocked_dropping_message")
	}
}

// HandleWS 处理 WebSocket 请求
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("ws_upgrade_failed", slog.String("error", err.Error()))
		return
	}
	client := &Client{hub: h, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	// 启动写泵（发送消息给前端）
	go client.writePump()
	// 启动读泵（处理心跳）
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		if c.hub.OnActivity != nil {
			c.hub.OnActivity() // Pong is also an activity
		}
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		if c.hub.OnActivity != nil {
			c.hub.OnActivity() // Incoming message is activity
		}

		// 🎨 Handle custom messages from client
		var msg struct {
			Type string `json:"type"`
			Data struct {
				Address string `json:"address"`
				Status  string `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(message, &msg); err == nil {
			if msg.Type == "NEED_METADATA" && c.hub.OnNeedMeta != nil {
				c.hub.OnNeedMeta(msg.Data.Address)
			}
			if msg.Type == "HEARTBEAT" && c.hub.OnActivity != nil {
				c.hub.OnActivity()
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				c.hub.logger.Warn("ws_write_error", slog.String("err", err.Error()))
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
