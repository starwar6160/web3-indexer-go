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

	"github.com/jmoiron/sqlx"
)

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
	if ta.total < 100 {
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

var botUARegex = regexp.MustCompile(`(?i)(bot|crawler|spider|curl|wget|python|postman)`)

type visitorLogTask struct {
	db   *sqlx.DB
	ip   string
	ua   string
	path string
}

var visitorLogQueue = make(chan visitorLogTask, 2048)

func init() {
	for i := 0; i < 4; i++ {
		go func() {
			for task := range visitorLogQueue {
				logVisitor(task.db, task.ip, task.ua, task.path)
			}
		}()
	}
}

func VisitorStatsMiddleware(dbGetter func() *sqlx.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		} else {
			ip = strings.TrimSpace(ip)
			if idx := strings.Index(ip, ","); idx != -1 {
				ip = strings.TrimSpace(ip[:idx])
			}
		}

		ua := r.UserAgent()
		globalAnalyzer.Record(ip)

		if ip == globalAnalyzer.GetAdminIP() || ip == "127.0.0.1" {
			next.ServeHTTP(w, r)
			return
		}

		isBot := botUARegex.MatchString(ua)
		if strings.Contains(ua, "Mozilla") && !isBot && r.Method == http.MethodGet {
			db := dbGetter()
			if db != nil {
				task := visitorLogTask{db: db, ip: ip, ua: ua, path: r.URL.Path}
				select {
				case visitorLogQueue <- task:
				default:
					slog.Warn("visitor_log_queue_full_drop", "path", r.URL.Path)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func logVisitor(db *sqlx.DB, ip, ua, path string) {
	metadata := map[string]interface{}{"path": path, "recorded_v": "v1"}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		slog.Error("failed_to_marshal_metadata", "err", err)
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		_, err := db.Exec("INSERT INTO visitor_stats (ip_address, user_agent, metadata) VALUES ($1, $2, $3)", ip, ua, metaJSON)
		if err == nil {
			return
		}
		time.Sleep(time.Millisecond * 100 * time.Duration(attempt+1))
	}
}
