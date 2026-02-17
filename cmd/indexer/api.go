package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/web"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// REST Models
type Block struct {
	ProcessedAt string `db:"processed_at" json:"processed_at"`
	Number      string `db:"number" json:"number"`
	Hash        string `db:"hash" json:"hash"`
	ParentHash  string `db:"parent_hash" json:"parent_hash"`
	Timestamp   string `db:"timestamp" json:"timestamp"`
}

type Transfer struct {
	ID           int    `db:"id" json:"id"`
	BlockNumber  string `db:"block_number" json:"block_number"`
	TxHash       string `db:"tx_hash" json:"tx_hash"`
	LogIndex     int    `db:"log_index" json:"log_index"`
	FromAddress  string `db:"from_address" json:"from_address"`
	ToAddress    string `db:"to_address" json:"to_address"`
	Amount       string `db:"amount" json:"amount"`
	TokenAddress string `db:"token_address" json:"token_address"`
	Symbol       string `db:"symbol" json:"symbol"`      // ✅ 代币符号
	Type         string `db:"activity_type" json:"type"` // ✅ 新增：活动类型
}

// Server 包装 HTTP 服务
type Server struct {
	db          *sqlx.DB
	wsHub       *web.Hub
	port        string
	title       string
	rpcPool     engine.RPCClient
	lazyManager *engine.LazyManager
	signer      *engine.SignerMachine
	chainID     int64
	mu          sync.RWMutex
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
func (s *Server) SetDependencies(db *sqlx.DB, rpcPool engine.RPCClient, lazyManager *engine.LazyManager, chainID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	s.rpcPool = rpcPool
	s.lazyManager = lazyManager
	s.chainID = chainID
	slog.Info("💉 API Server dependencies injected")
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// 静态资源
	mux.Handle("/static/", web.HandleStatic())

	// API 路由 (使用闭包延迟访问依赖)
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
		s.mu.RUnlock()
		if db == nil {
			http.Error(w, "System Initializing...", 503)
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
			// 返回最小化的初始化状态
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"state": "initializing",
				"title": s.title,
				"msg":   "Database or RPC not ready yet",
			}); err != nil {
				slog.Error("failed_to_encode_init_status", "err", err)
			}
			return
		}
		handleGetStatus(w, r, db, rpcPool, lazyManager, chainID, s.signer)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.wsHub.HandleWS(w, r)
	})

	// 首页
	mux.HandleFunc("/", web.RenderDashboard)
	mux.HandleFunc("/security", web.RenderSecurity)

	// Prometheus 指标
	mux.Handle("/metrics", promhttp.Handler())

	slog.Info("🌐 Server listening", "port", s.port)
	srv := &http.Server{
		Addr:              ":" + s.port,
		Handler:           VisitorStatsMiddleware(nil, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

func handleGetBlocks(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	type dbBlock struct {
		ProcessedAt time.Time `db:"processed_at"`
		Number      string    `db:"number"`
		Hash        string    `db:"hash"`
		ParentHash  string    `db:"parent_hash"`
		Timestamp   string    `db:"timestamp"`
	}
	var rawBlocks []dbBlock
	// 强制要求字段顺序，并使用 AS 别名消除混淆
	err := db.SelectContext(r.Context(), &rawBlocks, `
		SELECT 
			number, 
			hash, 
			parent_hash, 
			timestamp, 
			processed_at 
		FROM blocks 
		ORDER BY number DESC 
		LIMIT 10
	`)
	if err != nil {
		slog.Error("failed_to_get_blocks", "err", err)
		http.Error(w, "Failed to retrieve blocks", 500)
		return
	}

	// 格式化时间戳为可读字符串 (15:04:05.000)
	blocks := make([]Block, len(rawBlocks))
	for i, b := range rawBlocks {
		blocks[i] = Block{
			Number:      b.Number,
			Hash:        b.Hash,
			ParentHash:  b.ParentHash,
			Timestamp:   b.Timestamp,
			ProcessedAt: b.ProcessedAt.Format("15:04:05.000"), // ⏳ 精确到毫秒的时刻
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"blocks": blocks}); err != nil {
		slog.Error("failed_to_encode_blocks", "err", err)
	}
}

func handleGetTransfers(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	var transfers []Transfer
	err := db.SelectContext(r.Context(), &transfers, "SELECT id, block_number, tx_hash, log_index, from_address, to_address, amount, token_address, symbol, activity_type FROM transfers ORDER BY block_number DESC, log_index DESC LIMIT 10")
	if err != nil {
		slog.Error("failed_to_get_transfers", "err", err)
		http.Error(w, "Failed to retrieve transfers", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"transfers": transfers}); err != nil {
		slog.Error("failed_to_encode_transfers", "err", err)
	}
}

// TrafficAnalyzer 内存滑动窗口分析器 (SRE Anomaly Detection)
type TrafficAnalyzer struct {
	mu        sync.RWMutex
	counts    map[string]int
	threshold float64
	total     int
}

func NewTrafficAnalyzer(threshold float64) *TrafficAnalyzer {
	return &TrafficAnalyzer{
		counts:    make(map[string]int),
		threshold: threshold,
	}
}

func (ta *TrafficAnalyzer) Record(ip string) {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	ta.counts[ip]++
	ta.total++

	// 定期清理窗口 (防止内存无限增长，每 2000 次请求重置一次)
	if ta.total > 2000 {
		for k := range ta.counts {
			delete(ta.counts, k)
		}
		ta.total = 0
	}
}

func (ta *TrafficAnalyzer) GetAdminIP() string {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	if ta.total < 100 { // 最小采样阈值
		return ""
	}
	for ip, count := range ta.counts {
		if float64(count)/float64(ta.total) > ta.threshold {
			return ip
		}
	}
	return ""
}

var globalAnalyzer = NewTrafficAnalyzer(0.9)

// VisitorStatsMiddleware 拦截流量并记录独立访客 (具备动态异常检测能力)
func VisitorStatsMiddleware(db *sqlx.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}

		// 更加鲁棒的 IP 解析，处理 IPv4/IPv6 以及端口号
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		} else {
			// 如果没有端口号（例如来自 X-Forwarded-For），SplitHostPort 会报错，直接使用原值
			ip = strings.TrimSpace(ip)
			// 如果是多个 IP（X-Forwarded-For: client, proxy1, proxy2），取第一个
			if idx := strings.Index(ip, ","); idx != -1 {
				ip = strings.TrimSpace(ip[:idx])
			}
		}

		ua := r.UserAgent()

		// 1. 更新分析器数据
		globalAnalyzer.Record(ip)

		// 2. 动态判定：排除占比过高的“异常 IP”（通常是管理员调试或压测源）
		if ip == globalAnalyzer.GetAdminIP() || ip == "127.0.0.1" {
			next.ServeHTTP(w, r)
			return
		}

		// 3. 判定是否为“人类浏览器”流量
		isBot := regexp.MustCompile(`(?i)(bot|crawler|spider|curl|wget|python|postman)`).MatchString(ua)
		isBrowser := strings.Contains(ua, "Mozilla")

		if isBrowser && !isBot && r.Method == http.MethodGet {
			// 4. 异步持久化 (仅当 DB 已就绪)
			if db != nil {
				go logVisitor(db, ip, ua, r.URL.Path)
			}
		}

		next.ServeHTTP(w, r)
	})
}

func logVisitor(db *sqlx.DB, ip, ua, path string) {
	metadata := map[string]interface{}{
		"path":       path,
		"recorded_v": "v1",
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		slog.Error("failed_to_marshal_metadata", "err", err)
		return
	}

	// Retry mechanism for database operations
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		_, err := db.Exec("INSERT INTO visitor_stats (ip_address, user_agent, metadata) VALUES ($1, $2, $3)",
			ip, ua, metaJSON)
		if err == nil {
			// Success, exit the retry loop
			return
		}

		// Log the error but don't spam if it's a connection issue
		if attempt < maxRetries-1 {
			slog.Warn("failed_to_log_visitor_retrying", "err", err, "ip", ip, "attempt", attempt+1)
			time.Sleep(time.Millisecond * 100 * time.Duration(attempt+1)) // Exponential backoff
		} else {
			// Final attempt failed, log the error
			slog.Error("failed_to_log_visitor_permanent", "err", err, "ip", ip, "attempts", maxRetries)
		}
	}
}

func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool engine.RPCClient, lazyManager *engine.LazyManager, _ int64, signer *engine.SignerMachine) {
	// Trigger indexing if cooldown period has passed
	if lazyManager != nil {
		slog.Debug("🚀 API access detected, triggering lazy manager")
		lazyManager.Trigger()
	}

	ctx := r.Context()
	// 1. 获取链上高度与同步高度
	latestChainBlock, err := rpcPool.GetLatestBlockNumber(ctx)
	if err != nil {
		slog.Error("failed_to_get_latest_block", "err", err)
	}
	latestIndexedBlock := getLatestIndexedBlock(ctx, db)

	// 2. 获取统计数据
	totalBlocks := getCount(ctx, db, "SELECT COUNT(*) FROM blocks")
	totalTransfers := getCount(ctx, db, "SELECT COUNT(*) FROM transfers")
	totalVisitors := getCount(ctx, db, "SELECT COUNT(DISTINCT ip_address) FROM visitor_stats")

	// 3. 计算延迟与状态
	latestChainInt64 := int64(0)
	if latestChainBlock != nil {
		latestChainInt64 = latestChainBlock.Int64()
	}
	latestIndexedBlockInt64 := parseBlockNumber(latestIndexedBlock)

	syncLag := latestChainInt64 - latestIndexedBlockInt64
	if syncLag < 0 {
		syncLag = 0
	}

	e2eLatencyDisplay, e2eLatencySeconds := calculateLatency(ctx, db, latestChainInt64, latestIndexedBlockInt64, latestIndexedBlock)

	// 4. 组装响应
	adminIP := globalAnalyzer.GetAdminIP()
	if adminIP != "" && adminIP != "127.0.0.1" {
		adminIP = "Protected-Internal-Node"
	}

	tps := calculateTPS(ctx, db)
	isCatchingUp := syncLag > 10
	if isCatchingUp {
		tps = 0.0
	}

	status := map[string]interface{}{
		"version":            "v2.2.0-intelligence-engine",
		"state":              "active",
		"latest_block":       fmt.Sprintf("%d", latestChainInt64),
		"latest_indexed":     latestIndexedBlock,
		"sync_lag":           syncLag,
		"total_blocks":       totalBlocks,
		"total_transfers":    totalTransfers,
		"total_visitors":     totalVisitors,
		"tps":                tps,
		"is_catching_up":     isCatchingUp,
		"bps":                currentBPS.Load(),
		"is_healthy":         rpcPool.GetHealthyNodeCount() > 0,
		"self_healing_count": selfHealingEvents.Load(),
		"admin_ip":           adminIP,
		"rpc_nodes": map[string]int{
			"healthy": rpcPool.GetHealthyNodeCount(),
			"total":   rpcPool.GetTotalNodeCount(),
		},
		"e2e_latency_seconds": e2eLatencySeconds,
		"e2e_latency_display": e2eLatencyDisplay,
	}

	if lazyManager != nil {
		status["lazy_indexer"] = lazyManager.GetStatus()
	}

	// 🛡️ 确定性安全签名
	if signer != nil {
		if signed, err := signer.Sign("status", status); err == nil {
			w.Header().Set("X-Payload-Signature", signed.Signature)
			w.Header().Set("X-Signer-ID", signed.SignerID)
			w.Header().Set("X-Public-Key", signed.PubKey)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Error("failed_to_encode_status", "err", err)
	}
}

func getLatestIndexedBlock(ctx context.Context, db *sqlx.DB) string {
	var latest string
	if err := db.GetContext(ctx, &latest, "SELECT COALESCE(MAX(number), '0') FROM blocks"); err != nil {
		return "0"
	}
	return latest
}

func getCount(ctx context.Context, db *sqlx.DB, query string) int64 {
	var count int64
	if err := db.GetContext(ctx, &count, query); err != nil {
		return 0
	}
	return count
}

func parseBlockNumber(s string) int64 {
	if s == "" || s == "0" {
		return 0
	}
	if parsed, ok := new(big.Int).SetString(s, 10); ok {
		return parsed.Int64()
	}
	return 0
}

func calculateLatency(ctx context.Context, db *sqlx.DB, latestChain, latestIndexed int64, latestIndexedStr string) (string, float64) {
	if latestChain <= 0 || latestIndexed <= 0 {
		return "0s", 0
	}

	syncLag := latestChain - latestIndexed
	if syncLag < 0 {
		syncLag = 0
	}

	// 🚀 工业级防御：如果落后太多（>100块），直接按区块平均时间估算
	if syncLag > 100 {
		estLatency := float64(syncLag) * 12
		return fmt.Sprintf("Catching up... (%d blocks behind)", syncLag), estLatency
	}

	// 实时/小延迟模式：尝试从数据库获取最新区块的处理时间
	var processedAt time.Time
	err := db.GetContext(ctx, &processedAt, "SELECT processed_at FROM blocks WHERE number = $1", latestIndexedStr)

	if err == nil && !processedAt.IsZero() {
		latency := time.Since(processedAt).Seconds()
		// 🛡️ 异常防御：如果计算出的延迟超过了理论上限（比如 Anvil 重启导致的巨大时间差），进行平滑处理
		maxExpectedLatency := float64(syncLag+1) * 15 // 允许一定的 Buffer
		if latency > maxExpectedLatency && syncLag < 5 {
			// 如果只有几个块的延迟，但时间差巨大，说明是环境重置
			latency = float64(syncLag) * 2.0 // 给一个较小的假定值
		}

		if latency < 0 {
			latency = 0
		}
		return fmt.Sprintf("%.2fs", latency), latency
	}

	// Fallback: 纯理论估算
	fallbackLatency := float64(syncLag) * 12
	return fmt.Sprintf("%.2fs", fallbackLatency), fallbackLatency
}

// calculateTPS 计算 Transactions Per Second
func calculateTPS(ctx context.Context, db *sqlx.DB) float64 {
	// 🚀 工业级对齐：直接从 Metrics 的 5s 滑动窗口获取
	return engine.GetMetrics().GetWindowTPS()
}

// getLazyIndexerStatus returns a human-readable status for the lazy indexer
func getLazyIndexerStatus(isActive bool) string {
	if isActive {
		return "● 正在追赶中 (Catching up...)"
	}
	return "● 节能模式 (Lazy Mode)"
}
