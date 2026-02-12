#!/bin/bash
# ==============================================================================
# SSH 安全加固脚本
# 1. 禁用密码登录 (强制密钥)
# 2. 禁用 Root 直接登录
# 3. 显式开启公钥认证
# ==============================================================================

set -e

SSHD_CONF="/etc/ssh/sshd_config"

echo "🔐 正在进行 SSH 安全加固..."

# 1. 备份原配置
sudo cp $SSHD_CONF "${SSHD_CONF}.bak.$(date +%F_%T)"

# 2. 修改配置
# 确保 Port 29875 存在 (防止误操作)
if ! grep -q "^Port 29875" $SSHD_CONF; then
    echo "⚠️ 警告：当前配置中未发现 Port 29875，请手动核实端口后再运行。"
    exit 1
fi

# 禁用密码登录
sudo sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' $SSHD_CONF
# 禁用 Root 登录
sudo sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin no/' $SSHD_CONF
# 开启公钥认证
sudo sed -i 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' $SSHD_CONF

echo "🧪 正在测试 SSH 配置语法..."
sudo sshd -t

echo "🔄 正在重启 SSH 服务..."
sudo systemctl restart ssh

echo "✅ SSH 加固完成！"
echo "⚠️  重要提醒：在断开当前连接前，请务必开启一个新终端测试是否能通过密钥正常登录！"
