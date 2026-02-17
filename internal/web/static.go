package web

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
)

//go:embed dashboard.html dashboard.js dashboard.css security.html PUBLIC_KEY.asc README.md.asc
var StaticAssets embed.FS

// HandleStatic 返回静态资源处理器
func HandleStatic() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(StaticAssets)))
}

// RenderDashboard 渲染主页
func RenderDashboard(w http.ResponseWriter, r *http.Request) {
	// 1. 读取模板
	tmplContent, err := StaticAssets.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "Dashboard template not found", http.StatusNotFound)
		return
	}

	tmpl, err := template.New("dashboard").Parse(string(tmplContent))
	if err != nil {
		http.Error(w, "Template parsing error", http.StatusInternalServerError)
		return
	}

	// 2. 环境识别逻辑 (使用物理 ChainID 判定)
	chainIDStr := os.Getenv("CHAIN_ID")

	// 默认值
	envName := "demo"
	dsName := "Web3-demo-DB"

	// 判定是否为 Sepolia (11155111)
	if chainIDStr == "11155111" {
		envName = "sepolia"
		dsName = "Web3-sepolia-DB"
	}

	title := os.Getenv("APP_TITLE")
	if title == "" {
		title = "🚀 Web3 Indexer Dashboard"
	}

	// 获取 Grafana 基础地址 (支持 Tailscale 动态识别)
	grafanaHost := os.Getenv("GRAFANA_HOST")
	if grafanaHost == "" {
		// 默认回退逻辑
		grafanaHost = r.URL.Hostname()
		if grafanaHost == "" {
			grafanaHost = "localhost"
		}
	}

	data := struct {
		Title       string
		Environment string
		PostgresDS  string
		GrafanaHost string
		Version     string
	}{
		Title:       title,
		Environment: envName,
		PostgresDS:  dsName,
		GrafanaHost: grafanaHost,
		Version:     "v2.2.0-intelligence-engine",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("failed_to_execute_template", "err", err)
	}
}

// RenderSecurity 渲染安全验证页
func RenderSecurity(w http.ResponseWriter, _ *http.Request) {
	data, err := StaticAssets.ReadFile("security.html")
	if err != nil {
		http.Error(w, "Security page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(data); err != nil {
		slog.Error("failed_to_write_security_page", "err", err)
	}
}
