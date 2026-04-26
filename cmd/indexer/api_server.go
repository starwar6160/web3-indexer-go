package main

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/web"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server 包装 HTTP 服务
type Server struct {
	db          *sqlx.DB
	wsHub       *web.Hub
	port        string
	title       string
	rpcPool     engine.RPCClient
	lazyManager *engine.LazyManager
	processor   *engine.Processor // 🚀 新增：用于访问 HotBuffer
	signer      *engine.SignerMachine
	chainID     int64
	mu          sync.RWMutex
	srv         *http.Server
}

func NewServer(db *sqlx.DB, wsHub *web.Hub, port, title string) *Server {
	return &Server{
		db:     db,
		wsHub:  wsHub,
		port:   port,
		title:  title,
		signer: engine.NewSignerMachine("Yokohama-Lab-Primary"),
	}
}

// SetDependencies 动态注入运行期依赖
func (s *Server) SetDependencies(db *sqlx.DB, rpcPool engine.RPCClient, lazyManager *engine.LazyManager, processor *engine.Processor, chainID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	s.rpcPool = rpcPool
	s.lazyManager = lazyManager
	s.processor = processor
	s.chainID = chainID
	slog.Info("💉 API Server dependencies injected")
}

func (s *Server) Start() error {
	slog.Info("🚀 STARTING SERVER V2 - CONFIGURING ROUTES")
	mux := http.NewServeMux()

	// 静态资源
	mux.Handle("/static/", web.HandleStatic())

	// API 路由
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		db := s.db
		s.mu.RUnlock()
		if db == nil {
			http.Error(w, "System Initializing...", 503)
			return
		}
		handleGetBlocks(w, r, db)
	})

	mux.HandleFunc("/api/transfers", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		db := s.db
		processor := s.processor
		s.mu.RUnlock()

		if db == nil {
			http.Error(w, "System Initializing...", 503)
			return
		}

		if processor != nil && processor.GetHotBuffer() != nil && processor.GetHotBuffer().GetCount() > 0 {
			handleGetTransfersFromHotBuffer(w, processor)
			return
		}

		handleGetTransfers(w, r, db)
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		db := s.db
		rpcPool := s.rpcPool
		lazyManager := s.lazyManager
		chainID := s.chainID
		s.mu.RUnlock()

		if db == nil || rpcPool == nil {
			handleInitialStatus(w, s.title)
			return
		}
		handleGetStatus(w, r, db, rpcPool, lazyManager, chainID, s.signer)
	})

	mux.HandleFunc("/api/debug/snapshot", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		db := s.db
		rpcPool := s.rpcPool
		s.mu.RUnlock()

		if db == nil || rpcPool == nil {
			http.Error(w, "System Initializing...", 503)
			return
		}
		handleGetDebugSnapshot(w, r, db, rpcPool)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.wsHub.HandleWS(w, r)
	})

	// 首页
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		web.RenderDashboard(w, r)
	})
	mux.HandleFunc("/security", web.RenderSecurity)

	// Prometheus 指标
	mux.Handle("/metrics", promhttp.Handler())

	slog.Info("🌐 Server listening", "port", s.port)
	s.mu.Lock()
	s.srv = &http.Server{
		Addr: ":" + s.port,
		Handler: VisitorStatsMiddleware(func() *sqlx.DB {
			s.mu.RLock()
			defer s.mu.RUnlock()
			return s.db
		}, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.mu.Unlock()
	return s.srv.ListenAndServe()
}

// Shutdown 优雅关闭 API 服务
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	srv := s.srv
	s.mu.RUnlock()

	if srv != nil {
		slog.Info("🌐 API Server shutting down...")
		return srv.Shutdown(ctx)
	}
	return nil
}
