#!/bin/bash

# --- 🚀 Yokohama Lab Environment Purge ---
# "One-click to forget everything and start fresh."

echo "🧹 Initiating full environment cleanup..."

# 1. 停止并移除所有相关容器及持久化卷
if [ -f "docker-compose.yml" ]; then
    echo "🐳 Terminating Docker infrastructure and wiping volumes..."
    docker-compose down -v
fi

# 2. 清理 Go 构建缓存与临时文件
echo "🐹 Cleaning Go cache and artifacts..."
go clean -cache -testcache -modcache
rm -rf bin/ logs/ tmp/ *.log

# 3. 重置本地 Anvil 状态 (如果正在运行)
if pgrep anvil > /dev/null; then
    echo "🔨 Resetting local Anvil process..."
    pkill anvil
    # 给一点时间释放端口
    sleep 2
fi

# 4. 重新初始化必要的目录
mkdir -p bin logs tmp

echo "✨ environment is now PRISTINE. Ready for a high-speed demo."
echo "💡 Usage: EPHEMERAL_MODE=true make a2"
