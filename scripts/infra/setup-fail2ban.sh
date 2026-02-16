#!/bin/bash
# ==============================================================================
# Fail2Ban SSH 加固脚本 (24小时封禁)
# ==============================================================================

set -e

JAIL_CONF="/etc/fail2ban/jail.local"

echo "🛡️  正在配置 Fail2Ban 规则..."

# 使用 sudo 写入系统配置
sudo bash -c "cat > $JAIL_CONF" <<EOF
[sshd]
enabled = true
port = 29875
filter = sshd
logpath = /var/log/auth.log
# 只要对方尝试 3 次失败
maxretry = 3
# 监测 10 分钟内的尝试
findtime = 10m
# 直接封掉 24 小时
bantime = 24h
# 封禁动作
action = iptables-multiport[name=sshd, port="29875", protocol=tcp]
EOF

echo "🔄 正在重启 Fail2Ban 服务..."
sudo systemctl restart fail2ban

echo "📊 当前 Fail2Ban 状态 (sshd):"
sudo fail2ban-client status sshd

echo "✅ 配置完成！攻击者现在只要敢试 3 次，就会消失 24 小时。"
