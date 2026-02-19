#!/bin/bash
# 重置 Anvil 到创世块
# 用于每次测试前确保 Anvil 从 0 开始

set -euo pipefail

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo "🔄 重置 Anvil 到创世块..."
echo ""

# ⚠️  检查 8082 是否在运行
if docker ps | grep -q "web3-indexer-app"; then
    echo -e "${RED}⚠️  警告：8082 容器（正式版）正在运行！${NC}"
    echo "重置 Anvil 会清空 8082 的数据库并导致其 stalled！"
    echo ""
    read -p "是否继续？(y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${YELLOW}❌ 已取消${NC}"
        exit 1
    fi
    echo -e "${YELLOW}🛑 先停止 8082...${NC}"
    docker stop web3-indexer-app
    sleep 2
fi

# 1. 停止并删除容器
echo "1️⃣  停止旧 Anvil 容器..."
# 🚀 修正容器名称：从 web3-demo2-anvil 改为 web3-indexer-anvil (由 docker compose -p web3-indexer 决定)
docker stop web3-indexer-anvil 2>/dev/null || echo -e "${YELLOW}⚠️  Anvil 容器未运行${NC}"
docker rm web3-indexer-anvil 2>/dev/null || echo -e "${YELLOW}⚠️  Anvil 容器未创建${NC}"

# 1.5 清理数据库状态 (强制无状态)
echo "📂 清理数据库状态 (web3_demo)..."
docker exec -i web3-indexer-db psql -U postgres -p 15432 -d web3_demo -c "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;" || echo -e "${YELLOW}⚠️  数据库清理失败 (可能尚未启动)${NC}"

# 2. 重新启动
echo ""
echo "2️⃣  启动 Anvil..."
docker compose -p web3-indexer -f configs/docker/docker-compose.yml up -d anvil

# 3. 等待启动
echo ""
echo "3️⃣  等待 Anvil 启动..."
sleep 3

# 4. 验证高度
echo ""
echo "4️⃣  验证高度..."
HEIGHT=$(curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "%d")

echo -e "${GREEN}✅ Anvil 已重置，当前高度: $HEIGHT${NC}"

# 5. 重启 8082（如果之前停止了）
if docker ps -a | grep -q "web3-indexer-app"; then
    echo ""
    echo "5️⃣  重启 8082 容器..."
    docker start web3-indexer-app
    echo -e "${GREEN}✅ 8082 已重启${NC}"
fi

echo ""
echo "现在可以运行: make test-a2"
