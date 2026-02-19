#!/bin/bash
# 完全清理 8082 和 8092 的所有状态
# 使其回到完全无状态

set -euo pipefail

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}🔄 完全清理系统状态（无状态模式）${NC}"
echo "========================================"
echo ""

# 1. 停止所有相关容器
echo -e "${YELLOW}1️⃣  停止所有容器...${NC}"
echo "-----------------------------------"
docker stop web3-indexer-app 2>/dev/null && echo "  ✅ 8082 已停止" || echo "  ⚠️  8082 未运行"
docker stop web3-demo2-app 2>/dev/null && echo "  ✅ 8092 已停止" || echo "  ⚠️  8092 未运行"
docker stop web3-demo2-anvil 2>/dev/null && echo "  ✅ Anvil 已停止" || echo "  ⚠️  Anvil 未运行"
echo ""

# 2. 删除容器
echo -e "${YELLOW}2️⃣  删除容器（无状态）...${NC}"
echo "-----------------------------------"
docker rm web3-indexer-app 2>/dev/null && echo "  ✅ 8082 容器已删除" || echo "  ⚠️  8082 容器未创建"
docker rm web3-demo2-anvil 2>/dev/null && echo "  ✅ Anvil 容器已删除" || echo "  ⚠️  Anvil 容器未创建"
echo ""

# 3. 清理数据库
echo -e "${YELLOW}3️⃣  清理数据库...${NC}"
echo "-----------------------------------"
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo <<SQL
TRUNCATE TABLE blocks CASCADE;
TRUNCATE TABLE transfers CASCADE;
TRUNCATE TABLE sync_checkpoints CASCADE;
TRUNCATE TABLE sync_status CASCADE;
TRUNCATE TABLE visitor_stats CASCADE;
SQL
echo "  ✅ 数据库已清空"
echo ""

# 4. 重新启动 Anvil（临时模式）
echo -e "${YELLOW}4️⃣  启动 Anvil（临时模式）...${NC}"
echo "-----------------------------------"
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d anvil
echo "  ✅ Anvil 已启动（临时模式）"
echo ""

# 5. 等待 Anvil 启动
echo -e "${YELLOW}5️⃣  等待 Anvil 就绪...${NC}"
echo "-----------------------------------"
sleep 3

# 6. 验证 Anvil 高度
HEIGHT=$(curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' | xargs printf "%d")

echo -e "${GREEN}  ✅ Anvil 高度: $HEIGHT${NC}"
echo ""

# 7. 重新启动 8082
echo -e "${YELLOW}6️⃣  启动 8082（正式版）...${NC}"
echo "-----------------------------------"
docker compose -p web3-indexer -f configs/docker/docker-compose.yml --profile demo up -d indexer
echo -e "${GREEN}  ✅ 8082 已启动${NC}"
echo ""

# 8. 总结
echo -e "${GREEN}✅ 系统已清理完成（无状态模式）${NC}"
echo "========================================"
echo ""
echo "当前状态："
echo "  - Anvil: 临时模式（容器重启后从 0 开始）"
echo "  - 8082: 从块 0 开始同步"
echo "  - 8092: 使用 make test-a2 启动（也从 0 开始）"
echo ""
echo "验证命令："
echo "  - Anvil 高度: curl -s http://localhost:8545 ..."
echo "  - 8082 状态:   curl -s http://localhost:8082/api/status"
echo "  - 8092 状态:   make test-a2"
echo ""
