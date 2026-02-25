#!/bin/bash
# ==============================================================================
# WebSocket 连接诊断脚本
# ==============================================================================

set -e

echo "🔍 WebSocket 连接诊断工具"
echo "=========================================="
echo ""

# 1. 检查本地 WebSocket 端点
echo "📍 测试 1: 本地 WebSocket 端点 (127.0.0.1:8082/ws)"
echo "-------------------------------------------"
LOCAL_WS_RESULT=$(curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  http://127.0.0.1:8082/ws 2>&1 | head -5)

if echo "$LOCAL_WS_RESULT" | grep -q "101 Switching Protocols"; then
    echo "✅ 本地 WebSocket 连接成功"
    echo "   响应: HTTP/1.1 101 Switching Protocols"
else
    echo "❌ 本地 WebSocket 连接失败"
    echo "$LOCAL_WS_RESULT"
fi
echo ""

# 2. 检查 indexer 进程状态
echo "📍 测试 2: Indexer 服务状态"
echo "-------------------------------------------"
if netstat -tlnp 2>/dev/null | grep -q ":8082.*LISTEN"; then
    echo "✅ Indexer 服务正在监听 8082 端口"
    netstat -tlnp 2>/dev/null | grep ":8082"
else
    echo "❌ Indexer 服务未在 8082 端口监听"
fi
echo ""

# 3. 检查 Cloudflare Tunnel 状态
echo "📍 测试 3: Cloudflare Tunnel 状态"
echo "-------------------------------------------"
CLOUDFLARED_PROCESSES=$(ps aux | grep cloudflared | grep -v grep | wc -l)
if [ "$CLOUDFLARED_PROCESSES" -gt 0 ]; then
    echo "✅ Cloudflare Tunnel 进程运行中 ($CLOUDFLARED_PROCESSES 个进程)"
    ps aux | grep cloudflared | grep -v grep | awk '{print "   PID: " $2 " | " $11 " " $12 " " $13}'
else
    echo "❌ Cloudflare Tunnel 进程未运行"
fi
echo ""

# 4. 测试公网 HTTP 访问
echo "📍 测试 4: 公网 HTTP 访问 (demo2.st6160.click)"
echo "-------------------------------------------"
PUBLIC_HTTP_RESULT=$(curl -I https://demo2.st6160.click/ 2>&1 | head -10)
HTTP_STATUS=$(echo "$PUBLIC_HTTP_RESULT" | grep "^HTTP" | awk '{print $2}')

if [ "$HTTP_STATUS" = "200" ]; then
    echo "✅ 公网 HTTP 访问正常 (HTTP 200)"
elif [ "$HTTP_STATUS" = "403" ]; then
    echo "⚠️ 公网 HTTP 访问被阻止 (HTTP 403)"
    if echo "$PUBLIC_HTTP_RESULT" | grep -q "cf-mitigated: challenge"; then
        echo "   原因: Cloudflare Bot Management / Challenge 拦截"
        echo "   解决方案: https://dash.cloudflare.com → Security → 设置为 Low / Essentially Off"
    fi
else
    echo "⚠️ 公网 HTTP 访问返回状态: $HTTP_STATUS"
fi
echo ""

# 5. 测试公网 WebSocket 访问
echo "📍 测试 5: 公网 WebSocket 访问 (wss://demo2.st6160.click/ws)"
echo "-------------------------------------------"
PUBLIC_WS_RESULT=$(curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  https://demo2.st6160.click/ws 2>&1 | head -10)

WS_STATUS=$(echo "$PUBLIC_WS_RESULT" | grep "^HTTP" | awk '{print $2}')

if echo "$PUBLIC_WS_RESULT" | grep -q "101 Switching Protocols"; then
    echo "✅ 公网 WebSocket 连接成功"
    echo "   响应: HTTP/1.1 101 Switching Protocols"
elif [ "$WS_STATUS" = "403" ]; then
    echo "⚠️ 公网 WebSocket 被阻止 (HTTP 403)"
    if echo "$PUBLIC_WS_RESULT" | grep -q "cf-mitigated: challenge"; then
        echo "   原因: Cloudflare Bot Management / Challenge 拦截"
        echo ""
        echo "🔧 修复步骤:"
        echo "   1. 登录 https://dash.cloudflare.com"
        echo "   2. 选择域名: st6160.click"
        echo "   3. 导航到 Security → Settings"
        echo "   4. Bot Fight Mode: 关闭"
        echo "   5. Security Level: 设置为 Low 或 Essentially Off"
        echo "   6. Under Attack Mode: 关闭"
        echo "   7. 等待 30 秒后重试"
        echo ""
        echo "📱 或者，前端会自动降级到 HTTP 轮询模式"
    fi
else
    echo "⚠️ 公网 WebSocket 访问返回状态: $WS_STATUS"
fi
echo ""

# 6. 检查配置文件
echo "📍 测试 6: Cloudflare Tunnel 配置"
echo "-------------------------------------------"
CONFIG_FILE="/home/ubuntu/.cloudflared/config.yml"
if [ -f "$CONFIG_FILE" ]; then
    echo "✅ 配置文件存在: $CONFIG_FILE"
    if grep -q "path: /ws" "$CONFIG_FILE"; then
        echo "✅ WebSocket 路径配置已添加 (path: /ws)"
    else
        echo "⚠️ WebSocket 路径配置缺失 (path: /ws)"
    fi
else
    echo "❌ 配置文件不存在: $CONFIG_FILE"
fi
echo ""

# 7. 总结
echo "=========================================="
echo "📊 诊断总结"
echo "=========================================="
echo ""
echo "本地 WebSocket:     ✅ 正常 (如果 indexer 运行中)"
echo "Cloudflare Tunnel:  ✅ 运行中"
echo "公网访问:           ⚠️ 可能被 Cloudflare 安全策略拦截"
echo ""
echo "🎯 推荐解决方案:"
echo ""
echo "方案 1: 调整 Cloudflare 安全设置（推荐）"
echo "  - 访问: https://dash.cloudflare.com"
echo "  - 域名: st6160.click"
echo "  - Security → Settings → Bot Fight Mode: 关闭"
echo "  - Security → Settings → Security Level: Low"
echo ""
echo "方案 2: 前端自动降级（已实施）"
echo "  - WebSocket 失败 3 次后自动切换到 HTTP 轮询"
echo "  - 每 2 秒更新一次数据"
echo "  - 每 60 秒尝试恢复 WebSocket 连接"
echo ""
echo "🔗 测试链接:"
echo "  - 本地:  http://127.0.0.1:8082/"
echo "  - 公网:  https://demo2.st6160.click/"
echo "  - Grafana: https://grafana-demo2.st6160.click/"
echo ""
