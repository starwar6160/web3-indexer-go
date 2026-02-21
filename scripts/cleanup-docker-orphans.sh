#!/bin/bash
set -e

echo "🔍 Docker 容器清理脚本 - 彻底隔离 Demo2 项目"
echo "================================================================"
echo ""

# 检查当前容器状态
echo "📋 当前所有 web3- 容器："
docker ps -a --filter "name=web3-" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' || echo "无 web3- 容器"
echo ""

# 显示当前 web3-indexer-app 的 project name
echo "🔍 检查当前 web3-indexer-app 的项目标识..."
CURRENT_PROJECT=$(docker inspect web3-indexer-app --format '{{index .Config.Labels "com.docker.compose.project"}}' 2>/dev/null || echo "unknown")
echo "   当前 project name: $CURRENT_PROJECT"
echo ""

# 显示端口占用
echo "🔍 当前端口占用："
netstat -tulpn 2>/dev/null | grep -E ':(8081|8082|8545|15432|15434|3000|9090)' | awk '{print "   " $4 " → " $7}' || \
ss -tulpn 2>/dev/null | grep -E ':(8081|8082|8545|15432|15434|3000|9090)' | awk '{print "   " $5 " → " $6}' || \
echo "   无法获取端口信息"
echo ""

# 显示 API 状态
echo "🔍 API 服务状态："
echo -n "   Demo2 (8082): "
if curl -s http://localhost:8082/api/status > /dev/null 2>&1; then
    CHAIN_ID=$(curl -s http://localhost:8082/api/status | jq -r '.chain_id // "unknown"')
    SYNC_STATE=$(curl -s http://localhost:8082/api/status | jq -r '.sync_state // "unknown"')
    echo "✅ 运行中 (ChainID: $CHAIN_ID, State: $SYNC_STATE)"
else
    echo "❌ 无法连接"
fi

echo -n "   Testnet (8081): "
if curl -s http://localhost:8081/api/status > /dev/null 2>&1; then
    CHAIN_ID=$(curl -s http://localhost:8081/api/status | jq -r '.chain_id // "unknown"')
    echo "✅ 运行中 (ChainID: $CHAIN_ID)"
else
    echo "❌ 无法连接"
fi
echo ""

# 确认操作
echo "⚠️  即将执行以下操作："
echo "   1. 停止并删除所有 web3-indexer-* 容器（包括当前的 app）"
echo "   2. 保留 web3-testnet-app 容器（测试网项目）"
echo "   3. 更新 .env.demo2，设置 COMPOSE_PROJECT_NAME=indexer-demo"
echo "   4. 重启 Demo2 项目（使用新的 indexer-demo-* 容器名）"
echo ""

read -p "确认继续？(yes/no): " CONFIRM
if [ "$CONFIRM" != "yes" ]; then
    echo "❌ 操作已取消"
    exit 0
fi

echo ""
echo "🛑 开始清理..."

# 步骤 1：停止当前 Demo2 项目
echo ""
echo "📌 步骤 1/4：停止当前 Demo2 项目..."
docker-compose -f configs/docker/docker-compose.yml \
  --project-name web3-indexer down --remove-orphans 2>/dev/null || \
docker-compose -f configs/docker/docker-compose.yml \
  --project-name web3-indexer down 2>/dev/null || \
echo "   ℹ️  Docker Compose 项目已停止或不存在"

# 步骤 2：手动删除残留的 web3-indexer-* 容器
echo ""
echo "📌 步骤 2/4：删除残留的 web3-indexer-* 容器..."
REMAINING_CONTAINERS=$(docker ps -a --format '{{.Names}}' | grep '^web3-indexer-' || true)

if [ -n "$REMAINING_CONTAINERS" ]; then
    echo "   发现残留容器："
    for container in $REMAINING_CONTAINERS; do
        echo "     - 删除 $container"
        docker stop "$container" 2>/dev/null || true
        docker rm "$container" 2>/dev/null || true
    done
else
    echo "   ℹ️  无残留容器"
fi

# 步骤 3：更新 .env.demo2
echo ""
echo "📌 步骤 3/4：更新 .env.demo2 配置..."
ENV_FILE="configs/env/.env.demo2"

if [ -f "$ENV_FILE" ]; then
    # 检查是否已存在 COMPOSE_PROJECT_NAME
    if grep -q "^COMPOSE_PROJECT_NAME=" "$ENV_FILE"; then
        # 更新现有值
        sed -i 's/^COMPOSE_PROJECT_NAME=.*/COMPOSE_PROJECT_NAME=indexer-demo/' "$ENV_FILE"
        echo "   ✅ 更新 COMPOSE_PROJECT_NAME=indexer-demo"
    else
        # 添加新行
        echo "" >> "$ENV_FILE"
        echo "# Docker Compose Project Name (for container isolation)" >> "$ENV_FILE"
        echo "COMPOSE_PROJECT_NAME=indexer-demo" >> "$ENV_FILE"
        echo "   ✅ 添加 COMPOSE_PROJECT_NAME=indexer-demo"
    fi
else
    echo "   ❌ 错误：$ENV_FILE 文件不存在"
    exit 1
fi

# 步骤 4：重启 Demo2 项目
echo ""
echo "📌 步骤 4/4：重启 Demo2 项目（使用 indexer-demo project name）..."
docker-compose -f configs/docker/docker-compose.yml \
  --project-name indexer-demo \
  --env-file "$ENV_FILE" \
  up -d

echo ""
echo "⏳ 等待容器启动..."
sleep 5

# 验证结果
echo ""
echo "================================================================"
echo "✅ 清理完成！验证结果："
echo "================================================================"
echo ""

echo "📊 Demo2 项目容器（indexer-demo-*）："
docker ps --filter "name=indexer-demo" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' || echo "   无 indexer-demo 容器"
echo ""

echo "📊 测试网项目容器（web3-testnet-*）："
docker ps --filter "name=web3-testnet" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' || echo "   无 web3-testnet 容器"
echo ""

echo "🔍 检查遗留的 web3-indexer-* 容器："
OLD_CONTAINERS=$(docker ps -a --format '{{.Names}}' | grep '^web3-indexer-' || true)
if [ -z "$OLD_CONTAINERS" ]; then
    echo "   ✅ 无遗留容器（清理成功）"
else
    echo "   ⚠️  发现遗留容器："
    echo "$OLD_CONTAINERS"
fi
echo ""

echo "🔍 API 服务验证："
echo -n "   Demo2 (8082): "
if curl -s http://localhost:8082/api/status > /dev/null 2>&1; then
    CHAIN_ID=$(curl -s http://localhost:8082/api/status | jq -r '.chain_id // "unknown"')
    SYNC_STATE=$(curl -s http://localhost:8082/api/status | jq -r '.sync_state // "unknown"')
    LATEST_BLOCK=$(curl -s http://localhost:8082/api/status | jq -r '.latest_block // "unknown"')
    echo "✅ 运行中"
    echo "      ChainID: $CHAIN_ID"
    echo "      Sync State: $SYNC_STATE"
    echo "      Latest Block: $LATEST_BLOCK"
else
    echo "❌ 无法连接（容器可能还在启动中）"
fi

echo ""
echo -n "   Testnet (8081): "
if curl -s http://localhost:8081/api/status > /dev/null 2>&1; then
    echo "✅ 运行中（未被影响）"
else
    echo "⚠️  无法连接"
fi

echo ""
echo "================================================================"
echo "🎉 环境隔离完成！"
echo "   - Demo2 项目: indexer-demo-* (8082)"
echo "   - Testnet 项目: web3-testnet-* (8081)"
echo "================================================================"
