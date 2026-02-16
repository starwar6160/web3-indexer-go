#!/bin/bash
# ==============================================================================
# Web3 Indexer 安全加固最终脚本
# 用于更新 systemd 服务配置并重启服务
# ==============================================================================

set -e

NEW_PASSWORD="W3b3_Idx_Secur3_2026_Sec"
SERVICE_FILE="/etc/systemd/system/web3-indexer.service"

echo "🔐 正在更新 systemd 服务文件中的数据库密码..."
if [ -f "$SERVICE_FILE" ]; then
    sudo sed -i "s|Environment=DATABASE_URL=.*|Environment=DATABASE_URL=postgres://postgres:${NEW_PASSWORD}@127.0.0.1:15432/web3_indexer?sslmode=disable|" "$SERVICE_FILE"
    echo "✅ 服务文件已更新。"
else
    echo "❌ 找不到服务文件: $SERVICE_FILE"
    exit 1
fi

echo "🔄 正在重新加载 systemd 配置..."
sudo systemctl daemon-reload

echo "🚀 正在重启 web3-indexer.service..."
sudo systemctl restart web3-indexer.service

echo "📊 当前服务状态："
sudo systemctl status web3-indexer.service --no-pager

echo "🛡️ 正在应用 iptables 防火墙补丁 (针对 Docker 绕过问题)..."
# 在 DOCKER-USER 链最前端增加规则
# 允许本地回环、已建立的连接、Tailscale 网络
sudo iptables -C DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || sudo iptables -I DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
sudo iptables -C DOCKER-USER -i lo -j ACCEPT 2>/dev/null || sudo iptables -I DOCKER-USER -i lo -j ACCEPT
sudo iptables -C DOCKER-USER -s 100.64.0.0/10 -j ACCEPT 2>/dev/null || sudo iptables -I DOCKER-USER -s 100.64.0.0/10 -j ACCEPT

# 默认拒绝来自物理网卡 (enp1s0) 的所有指向容器的请求
sudo iptables -C DOCKER-USER -i enp1s0 -j DROP 2>/dev/null || sudo iptables -A DOCKER-USER -i enp1s0 -j DROP

echo "✅ Iptables 防御规则已生效。"
echo ""
echo "🎉 所有安全加固步骤已完成！"
