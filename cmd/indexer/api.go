package main

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"web3-indexer-go/internal/engine"

	"github.com/jmoiron/sqlx"
)

// REST Models
type Block struct {
	Number      string    `db:"number" json:"number"`
	Hash        string    `db:"hash" json:"hash"`
	ParentHash  string    `db:"parent_hash" json:"parent_hash"`
	Timestamp   string    `db:"timestamp" json:"timestamp"`
	ProcessedAt time.Time `db:"processed_at" json:"processed_at"`
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
}

func handleGetBlocks(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	var blocks []Block
	err := db.SelectContext(r.Context(), &blocks, "SELECT number, hash, parent_hash, timestamp, processed_at FROM blocks ORDER BY number DESC LIMIT 10")
	if err != nil {
		slog.Error("failed_to_get_blocks", "err", err)
		http.Error(w, "Failed to retrieve blocks", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"blocks": blocks})
}

func handleGetTransfers(w http.ResponseWriter, r *http.Request, db *sqlx.DB) {
	var transfers []Transfer
	err := db.SelectContext(r.Context(), &transfers, "SELECT id, block_number, tx_hash, log_index, from_address, to_address, amount, token_address FROM transfers ORDER BY block_number DESC LIMIT 10")
	if err != nil {
		slog.Error("failed_to_get_transfers", "err", err)
		http.Error(w, "Failed to retrieve transfers", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"transfers": transfers})
}

// TrafficAnalyzer 内存滑动窗口分析器 (SRE Anomaly Detection)
type TrafficAnalyzer struct {
	mu        sync.RWMutex
	counts    map[string]int
	total     int
	threshold float64
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
			// 4. 异步持久化
			go logVisitor(db, ip, ua, r.URL.Path)
		}

		next.ServeHTTP(w, r)
	})
}

func logVisitor(db *sqlx.DB, ip, ua, path string) {
	metadata := map[string]interface{}{
		"path":       path,
		"recorded_v": "v1",
	}
	metaJSON, _ := json.Marshal(metadata)

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

func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool *engine.RPCClientPool) {
	latestChainBlock, err := rpcPool.GetLatestBlockNumber(r.Context())

	var latestIndexedBlock string
	err = db.GetContext(r.Context(), &latestIndexedBlock, "SELECT COALESCE(MAX(number), '0') FROM blocks")
	if err != nil {
		slog.Error("failed_to_get_latest_indexed_block", "err", err)
		latestIndexedBlock = "0"
	}

	var totalBlocks, totalTransfers int64
	err = db.GetContext(r.Context(), &totalBlocks, "SELECT COUNT(*) FROM blocks")
	if err != nil {
		slog.Error("failed_to_get_total_blocks", "err", err)
		totalBlocks = 0
	}

	err = db.GetContext(r.Context(), &totalTransfers, "SELECT COUNT(*) FROM transfers")
	if err != nil {
		slog.Error("failed_to_get_total_transfers", "err", err)
		totalTransfers = 0
	}

	var totalVisitors int64
	err = db.GetContext(r.Context(), &totalVisitors, "SELECT COUNT(DISTINCT ip_address) FROM visitor_stats")
	if err != nil {
		slog.Error("failed_to_get_total_visitors", "err", err)
		totalVisitors = 0
	}

	latestBlockStr := "0"
	var syncLag int64
	if latestChainBlock != nil {
		latestBlockStr = latestChainBlock.String()
		syncLag = latestChainBlock.Int64() - totalBlocks
		if syncLag < 0 {
			syncLag = 0
		}
	}

	adminIP := globalAnalyzer.GetAdminIP()
	if adminIP != "" && adminIP != "127.0.0.1" {
		// 隐私防御：抹除真实 IP，替换为固定占位符
		adminIP = "Protected-Internal-Node"
	}

	status := map[string]interface{}{
		"state":              "active",
		"latest_block":       latestBlockStr,
		"latest_indexed":     latestIndexedBlock,
		"sync_lag":           syncLag,
		"total_blocks":       totalBlocks,
		"total_transfers":    totalTransfers,
		"total_visitors":     totalVisitors,
		"tps":                currentTPS.Load(),
		"bps":                currentBPS.Load(),
		"is_healthy":         rpcPool.GetHealthyNodeCount() > 0,
		"self_healing_count": selfHealingEvents.Load(),
		"admin_ip":           adminIP,
		"rpc_nodes": map[string]int{
			"healthy": rpcPool.GetHealthyNodeCount(),
			"total":   rpcPool.GetTotalNodeCount(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
