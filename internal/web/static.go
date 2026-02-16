package web

import (
	"embed"
	"html/template"
	"net/http"
	"os"
	"strings"
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

	// 2. 环境识别逻辑 (SRE 工业级注入)
	title := os.Getenv("APP_TITLE")
	if title == "" {
		title = "🚀 Web3 Indexer Dashboard"
	}

	// 计算数据源名称 (必须与 Grafana 中定义的一致)
	// 如果是 Sepolia 环境，使用 Web3-sepolia-DB，否则使用 Web3-demo-DB
	isSepolia := strings.Contains(strings.ToUpper(title), "SEPOLIA") || strings.Contains(strings.ToUpper(title), "TESTNET")
	envName := "demo"
	dsName := "Web3-demo-DB"
	if isSepolia {
		envName = "sepolia"
		dsName = "Web3-sepolia-DB"
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
	}{
		Title:       title,
		Environment: envName,
		PostgresDS:  dsName,
		GrafanaHost: grafanaHost,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, data)
}

// RenderSecurity 渲染安全验证页
func RenderSecurity(w http.ResponseWriter, _ *http.Request) {
	data, err := StaticAssets.ReadFile("security.html")
	if err != nil {
		http.Error(w, "Security page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
