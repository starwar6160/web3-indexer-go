package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/big"
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
	ProcessedAt time.Time `db:"processed_at" json:"processed_at"`
	Number      string    `db:"number" json:"number"`
	Hash        string    `db:"hash" json:"hash"`
	ParentHash  string    `db:"parent_hash" json:"parent_hash"`
	Timestamp   string    `db:"timestamp" json:"timestamp"`
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"blocks": blocks}); err != nil {
		slog.Error("failed_to_encode_blocks", "err", err)
	}
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

func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool engine.RPCClient) {
	latestChainBlock, _ := rpcPool.GetLatestBlockNumber(r.Context())

	var latestIndexedBlock string
	var err error
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

	// 解析最新已同步区块号
	latestIndexedBlockInt64 := int64(0)
	if latestIndexedBlock != "" && latestIndexedBlock != "0" {
		if parsed, ok := new(big.Int).SetString(latestIndexedBlock, 10); ok {
			latestIndexedBlockInt64 = parsed.Int64()
		}
	}

	latestBlockStr := "0"
	var syncLag int64
	if latestChainBlock != nil {
		latestBlockStr = latestChainBlock.String()
		// 修复：正确计算同步滞后 = 链头高度 - 最新已同步区块号
		syncLag = latestChainBlock.Int64() - latestIndexedBlockInt64
		if syncLag < 0 {
			syncLag = 0
		}
	}

	// 计算 E2E Latency（秒）
	// 添加上限检测和修正机制
	var e2eLatencySeconds int64
	var e2eLatencyDisplay string // 友好的显示格式
	if latestChainBlock != nil && latestIndexedBlockInt64 > 0 {
		rawLatency := syncLag * 12 // Sepolia 平均出块时间 12 秒

		// 上限检测：最大显示 1 小时（3600 秒）
		// 超过 1 小时显示 "Catching up..."
		if rawLatency > 3600 {
			e2eLatencySeconds = 3600
			e2eLatencyDisplay = fmt.Sprintf("Catching up... %d blocks remaining", syncLag)
		} else if rawLatency <= 0 {
			// 实时模式：Sync Lag 很小（<= 10），尝试计算真实的索引延迟
			if syncLag <= 10 {
				// 查询最新已索引块的处理时间
				var processedAtStr string
				err = db.GetContext(r.Context(), &processedAtStr,
					"SELECT processed_at FROM blocks WHERE number = $1", latestIndexedBlock)
				if err == nil && processedAtStr != "" {
					processedAt, _ := time.Parse(time.RFC3339, processedAtStr)
					actualLatency := time.Since(processedAt).Seconds()
					e2eLatencySeconds = int64(actualLatency)
					e2eLatencyDisplay = fmt.Sprintf("%.1fs", actualLatency)
				} else {
					e2eLatencySeconds = rawLatency
					e2eLatencyDisplay = fmt.Sprintf("%ds", rawLatency)
				}
			} else {
				e2eLatencySeconds = rawLatency
				e2eLatencyDisplay = fmt.Sprintf("%ds", rawLatency)
			}
		} else {
			e2eLatencySeconds = rawLatency
			minutes := rawLatency / 60
			seconds := rawLatency % 60
			if minutes > 0 {
				e2eLatencyDisplay = fmt.Sprintf("%dm %ds", minutes, seconds)
			} else {
				e2eLatencyDisplay = fmt.Sprintf("%ds", seconds)
			}
		}
	}

	adminIP := globalAnalyzer.GetAdminIP()
	if adminIP != "" && adminIP != "127.0.0.1" {
		// 隐私防御：抹除真实 IP，替换为固定占位符
		adminIP = "Protected-Internal-Node"
	}

	status := map[string]interface{}{
		"state":                 "active",
		"latest_block":          latestBlockStr,
		"latest_indexed":        latestIndexedBlock,
		"sync_lag":              syncLag,
		"total_blocks":          totalBlocks,
		"total_transfers":       totalTransfers,
		"total_visitors":        totalVisitors,
		"tps":                   calculateTPS(totalTransfers, totalBlocks), // 实时 TPS
		"bps":                   currentBPS.Load(),
		"is_healthy":            rpcPool.GetHealthyNodeCount() > 0,
		"self_healing_count":    selfHealingEvents.Load(),
		"admin_ip":              adminIP,
		"rpc_nodes": map[string]int{
			"healthy": rpcPool.GetHealthyNodeCount(),
			"total":   rpcPool.GetTotalNodeCount(),
		},
		// E2E Latency（带上限检测和友好显示）
		"e2e_latency_seconds":  e2eLatencySeconds,
		"e2e_latency_display":  e2eLatencyDisplay,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Error("failed_to_encode_status", "err", err)
	}
}

// calculateTPS 计算 Transactions Per Second（基于历史数据）
// 保留 2 位小数，避免长浮点数显示
func calculateTPS(totalTransfers, totalBlocks int64) float64 {
	if totalBlocks == 0 {
		return 0.0
	}
	// 简化计算：平均每个区块的转账数 / 12 秒（Sepolia 出块时间）
	// 注意：这是历史平均值，不是实时速率
	avgTransfersPerBlock := float64(totalTransfers) / float64(totalBlocks)
	rawTPS := avgTransfersPerBlock / 12.0

	// 保留 2 位小数（四舍五入）
	return math.Round(rawTPS*100) / 100
}
