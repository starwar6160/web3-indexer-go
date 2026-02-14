# Grafana Cloudflare Tunnel 访问修复记录

## 问题描述

Grafana 通过 Cloudflare Tunnel 外网访问时无法加载：
- 主页面 `https://demo2.st6160.click/` 中的 iframe 无法显示 Grafana 仪表板
- 浏览器报错："使用不受支持的协议"
- 本地访问 `localhost:4000` 正常

## 根本原因

### 1. 域名配置错误

| 配置位置 | 错误值 | 正确值 |
|---------|--------|--------|
| `dashboard.html` iframe src | `grafana.demo2.st6160.click` | `grafana-demo2.st6160.click` |
| `/etc/cloudflared/config.yml` | `grafana.demo2.st6160.click` | `grafana-demo2.st6160.click` |

Cloudflare 通配符证书 `*.st6160.click` 只支持一级子域名，不支持 `grafana.demo2` 这种二级嵌套。

### 2. Grafana 配置缺失

- `serve_from_sub_path` 设置为 `true`，但实际是根路径访问
- 缺少 `GF_SERVER_*` 环境变量定义

### 3. Go Embed 文件系统

HTML 文件通过 `//go:embed` 嵌入到 `bin/indexer` 二进制中，修改源文件后需要重新编译。

## 修复步骤

### 1. Grafana 配置修复

**文件**: `grafana-config/grafana.ini`

```ini
[server]
http_addr = 0.0.0.0
root_url = https://grafana-demo2.st6160.click
serve_from_sub_path = false  # 改为 false（原为 true）
enable_gzip = true
```

**文件**: `docker-compose.infra.yml`

```yaml
environment:
  - GF_SECURITY_ADMIN_PASSWORD=W3b3_Idx_Secur3_2026_Sec
  - GF_SERVER_ROOT_URL=https://grafana-demo2.st6160.click  # 新增
  - GF_SERVER_DOMAIN=grafana-demo2.st6160.click           # 新增
  - GF_SERVER_SERVE_FROM_SUB_PATH=false                    # 新增
```

### 2. 前端 HTML 修复

**文件**: `internal/web/dashboard.html:139`

```html
<!-- 修改前 -->
<iframe src="https://grafana.demo2.st6160.click/d/..."></iframe>

<!-- 修改后 -->
<iframe src="https://grafana-demo2.st6160.click/d/..."></iframe>
```

重新编译二进制文件：
```bash
cd /home/ubuntu/zwCode/web3-indexer-go
go build -o bin/indexer ./cmd/indexer
```

### 3. Cloudflare Tunnel 配置修复

**文件**: `/etc/cloudflared/config.yml`（需要 sudo 权限）

```bash
sudo sed -i 's/grafana\.demo2\.st6160\.click/grafana-demo2.st6160.click/g' /etc/cloudflared/config.yml
```

```yaml
ingress:
  - hostname: demo2.st6160.click
    service: http://localhost:8080
  - hostname: grafana-demo2.st6160.click  # 修改这里
    service: http://localhost:4000
  - service: http_status:404
```

重启服务：
```bash
sudo systemctl restart cloudflared
sudo systemctl restart web3-indexer.service
```

## 验证

### 本地测试
```bash
# 测试 Grafana
curl -I http://127.0.0.1:4000/

# 测试主页面 HTML
curl -s http://127.0.0.1:8080/ | grep grafana

# 验证二进制包含新域名
strings bin/indexer | grep grafana-demo2
```

### 外网测试
```bash
# 测试 Cloudflare Tunnel
curl -I https://grafana-demo2.st6160.click/
```

预期返回 `HTTP/2 200` 和 Grafana 相关响应头。

### 浏览器验证

访问 `https://demo2.st6160.click/`，确认：
- iframe 正常显示 Grafana 仪表板
- 控制台无 SSL 错误
- 静态资源（JS/CSS）正常加载

## 架构说明

```
┌─────────────────────────────────────────────────────────┐
│                  浏览器访问                          │
│    https://demo2.st6160.click/                       │
│         (主页面包含 Grafana iframe)                    │
└────────────────┬────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────┐
│              Cloudflare Tunnel                         │
│  grafana-demo2.st6160.click → localhost:4000         │
│  demo2.st6160.click       → localhost:8080           │
└────────────────┬────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────┐
│              本地服务 (127.0.0.1)                     │
│  :4000 → Grafana (Docker 容器)                       │
│  :8080 → web3-indexer (Go 服务)                      │
└─────────────────────────────────────────────────────────┘
```

## 关键文件清单

| 文件 | 作用 | 修改内容 |
|-----|------|---------|
| `grafana-config/grafana.ini` | Grafana 配置 | `serve_from_sub_path = false` |
| `docker-compose.infra.yml` | 容器定义 | 添加 `GF_SERVER_*` 环境变量 |
| `internal/web/dashboard.html` | 主页面 HTML | 修正 iframe src 域名 |
| `bin/indexer` | Go 二进制 | 重新编译嵌入新 HTML |
| `/etc/cloudflared/config.yml` | Tunnel 配置 | 修正 hostname |

## 故障排查

### 如果仍显示 "使用不受支持的协议"

1. **清除浏览器缓存**：使用无痕模式或 Ctrl+Shift+Delete
2. **确认域名正确**：
   ```bash
   grep grafana /etc/cloudflared/config.yml
   ```
3. **确认二进制已更新**：
   ```bash
   strings bin/indexer | grep grafana-demo2
   ```

### 如果返回 403/404

1. **检查 Cloudflare 安全设置**：
   - 关闭 Bot Fight Mode
   - 关闭 Under Attack Mode
2. **确认服务运行**：
   ```bash
   systemctl status web3-indexer
   systemctl status cloudflared
   docker ps | grep grafana
   ```

## 参考

- [Grafana Server Configuration](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#server)
- [Cloudflare Tunnel Documentation](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/)
- [Go Embed Directive](https://pkg.go.dev/embed)
