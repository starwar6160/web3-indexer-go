#!/bin/bash

# ==============================================================================
# Web3 Indexer 演示环境一键设置脚本
# ==============================================================================

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Web3 Indexer 演示环境设置 ===${NC}"

# 1. 环境清理
echo -e "${YELLOW}Step 1: 清理旧容器...${NC}"
docker compose down -v --remove-orphans 2>/dev/null || true

# 2. 设置演示环境变量
echo -e "${YELLOW}Step 2: 加载演示配置...${NC}"

# 导出演示配置
export DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@db:5432/web3_indexer?sslmode=disable"
export RPC_URLS="http://anvil:8545"
export CHAIN_ID="31337"
export START_BLOCK="0"
export EMULATOR_ENABLED="true"
export EMULATOR_PRIVATE_KEY="ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
export DEMO_MODE="true"
export API_PORT="8080"
export LOG_LEVEL="info"
export FETCH_CONCURRENCY="10"
export FETCH_BATCH_SIZE="200"
export CHECKPOINT_BATCH="100"
export RETRY_QUEUE_SIZE="500"

echo -e "${GREEN}✅ 演示配置已加载${NC}"

# 3. 启动服务
echo -e "${YELLOW}Step 3: 启动演示环境...${NC}"
docker compose up --build -d

echo -e "${GREEN}✅ 演示环境已启动${NC}"

# 4. 等待服务就绪
echo -e "${YELLOW}Step 4: 等待服务就绪...${NC}"
until curl -s http://localhost:8080/healthz > /dev/null; do
    echo -n "."
    sleep 2
done
echo -e "\n${GREEN}✅ API 服务已就绪!${NC}"

echo -e "\n${BLUE}=== 演示环境已准备就绪 ===${NC}"
echo -e "1. 访问仪表板: ${GREEN}http://localhost:8080${NC}"
echo -e "2. 查看日志: ${GREEN}docker compose logs -f indexer${NC}"
echo -e "3. 停止服务: ${GREEN}docker compose down${NC}"