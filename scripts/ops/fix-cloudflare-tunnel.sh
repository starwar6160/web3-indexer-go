#!/bin/bash
# ==============================================================================
# Cloudflare Tunnel IPv4 Fix - Yokohama Lab Ops
# ==============================================================================

set -e

CONFIG_FILE="/tmp/cloudflared_fix.yml"

echo "📝 Generating optimized cloudflared configuration..."

cat <<EOF > $CONFIG_FILE
tunnel: web3-indexer-tunnel
credentials-file: /home/ubuntu/.cloudflared/c1b94896-e188-4d52-a233-7986375c3790.json

ingress:
  # WebSocket 路由（demo2 - 需要协议升级支持）
  - hostname: demo2.st6160.click
    path: /ws
    service: http://127.0.0.1:8082
    originRequest:
      noTLSVerify: true
      connectTimeout: 30s
      tcpKeepAlive: 30s
      keepAliveConnections: 100
      keepAliveTimeout: 90s

  # HTTP API 路由（demo2）
  - hostname: demo2.st6160.click
    service: http://127.0.0.1:8082

  # Grafana WebSocket 路由（/api/live/ws 端点）
  - hostname: grafana-demo2.st6160.click
    path: /api/live
    service: http://127.0.0.1:4000
    originRequest:
      noTLSVerify: true
      connectTimeout: 30s
      tcpKeepAlive: 30s

  # Grafana 主路由
  - hostname: grafana-demo2.st6160.click
    service: http://127.0.0.1:4000

  # debug 配置（8083 端口）
  - hostname: debug.st6160.click
    service: http://127.0.0.1:8083

  # Fallback
  - service: http_status:404
EOF

echo "🚀 Synchronizing configurations..."

# 1. Sync to system-wide location
sudo cp $CONFIG_FILE /etc/cloudflared/config.yml

# 2. Sync to user local location
mkdir -p /home/ubuntu/.cloudflared
cp $CONFIG_FILE /home/ubuntu/.cloudflared/config.yml

echo "♻️ Restarting Cloudflare Tunnel services..."

# 3. Restart the specific tunnel service
if systemctl is-active --quiet cloudflared-my-app-tunnel.service; then
    sudo systemctl restart cloudflared-my-app-tunnel.service
fi

# 4. Restart the general cloudflared service
if systemctl is-active --quiet cloudflared.service; then
    sudo systemctl restart cloudflared.service
fi

echo "✅ [SUCCESS] Cloudflare Tunnel is now forced to IPv4 (127.0.0.1)."
echo "💡 Please check https://grafana-demo2.st6160.click in 10 seconds."
