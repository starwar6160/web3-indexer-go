package web

import (
	"embed"
	"net/http"
)

//go:embed dashboard.html dashboard.js dashboard.css
var StaticAssets embed.FS

// HandleStatic 返回静态资源处理器
func HandleStatic() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(StaticAssets)))
}

// RenderDashboard 渲染主页
func RenderDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := StaticAssets.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
