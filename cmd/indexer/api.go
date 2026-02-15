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
			ProcessedAt: b.ProcessedAt.Format("15:04:05.000"),
		}
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

func handleGetStatus(w http.ResponseWriter, r *http.Request, db *sqlx.DB, rpcPool engine.RPCClient, lazyManager *engine.LazyManager, chainID int64) {
	// Trigger indexing if cooldown period has passed
	if lazyManager != nil {
		lazyManager.Trigger()
	}

	// 1. 尝试实时获取链头
	latestChainBlock, err := rpcPool.GetLatestBlockNumber(r.Context())
	
	// 2. 缓存降级逻辑：如果 RPC 失败（如限流），从数据库读取 Heartbeat 记录
	latestBlockStr := "0"
	var latestChainInt64 int64
	if err == nil && latestChainBlock != nil {
		latestChainInt64 = latestChainBlock.Int64()
		latestBlockStr = latestChainBlock.String()
	} else {
		// 从 sync_checkpoints 读取心跳缓存 (动态根据 chainID)
		var cachedBlock string
		err = db.GetContext(r.Context(), &cachedBlock, "SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
		if err == nil && cachedBlock != "" {
			latestBlockStr = cachedBlock
			if val, ok := new(big.Int).SetString(cachedBlock, 10); ok {
				latestChainInt64 = val.Int64()
			}
			slog.Debug("using_cached_chain_head", "height", latestBlockStr, "chain_id", chainID)
		}
	}

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

	latestIndexedBlockInt64 := int64(0)
	if latestIndexedBlock != "" && latestIndexedBlock != "0" {
		if parsed, ok := new(big.Int).SetString(latestIndexedBlock, 10); ok {
			latestIndexedBlockInt64 = parsed.Int64()
		}
	}

	var syncLag int64
	if latestChainInt64 > 0 {
		// 修复：使用缓存或实时的链头高度进行计算
		syncLag = latestChainInt64 - latestIndexedBlockInt64
		if syncLag < 0 {
			syncLag = 0
		}
	}

	// 计算 E2E Latency（秒）
	var e2eLatencySeconds float64
	var e2eLatencyDisplay string 
	if latestChainInt64 > 0 && latestIndexedBlockInt64 > 0 {
		// 估算逻辑
		syncLag := latestChainInt64 - latestIndexedBlockInt64
		if syncLag < 0 { syncLag = 0 }
		
		rawLatency := float64(syncLag) * 12 // Sepolia 平均出块时间

		if syncLag > 100 {
			// 大规模追赶模式：显示剩余块数
			e2eLatencySeconds = rawLatency
			e2eLatencyDisplay = fmt.Sprintf("Catching up... (%d blocks behind)", syncLag)
		} else {
			// 实时/小延迟模式：计算处理延迟
			var processedAt time.Time
			err = db.GetContext(r.Context(), &processedAt, 
				"SELECT processed_at FROM blocks WHERE number = $1", latestIndexedBlock)
			
			if err == nil && !processedAt.IsZero() {
				actualLatency := time.Since(processedAt).Seconds()
				e2eLatencySeconds = actualLatency
				e2eLatencyDisplay = fmt.Sprintf("%.2fs", actualLatency)
			} else {
				e2eLatencySeconds = rawLatency
				e2eLatencyDisplay = fmt.Sprintf("%.2fs", rawLatency)
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
	
	// Add lazy indexer status if available
	if lazyManager != nil {
		lazyStatus := lazyManager.GetStatus()
		status["lazy_indexer"] = lazyStatus
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

// getLazyIndexerStatus returns a human-readable status for the lazy indexer
func getLazyIndexerStatus(isActive bool) string {
	if isActive {
		return "● 正在追赶中 (Catching up...)"
	}
	return "● 节能模式 (Lazy Mode)"
}
