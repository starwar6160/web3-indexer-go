#!/bin/bash

# ==============================================================================
# Web3 Indexer 一键开发运行脚本 (兼容 Docker 基础设施版)
# ==============================================================================

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Web3 Indexer 开发环境启动中 ===${NC}"

# 1. 清理旧的 Go 进程 (仅清理当前用户拥有的进程)
echo -e "${YELLOW}Step 1: 清理旧的 Indexer 进程...${NC}"
pkill -u $(whoami) -f "indexer" 2>/dev/null || true
echo -e "${GREEN}Indexer 进程已清理${NC}"

# 2. 启动/检查 Docker 基础设施
echo -e "${YELLOW}Step 2: 检查 Docker 基础设施 (Postgres + Anvil)...${NC}"

# 启动 Anvil 和 DB (如果已运行则保持现状)
docker compose up -d db anvil

# 等待 Anvil 就绪
echo -e "${YELLOW}等待 Anvil (8545) 就绪...${NC}"
MAX_RETRIES=15
COUNT=0
while ! curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' http://localhost:8545 > /dev/null; do
    sleep 1
    COUNT=$((COUNT + 1))
    if [ $COUNT -ge $MAX_RETRIES ]; then
        echo -e "\n${RED}错误: Anvil 启动超时。请尝试运行 'docker compose restart anvil'${NC}"
        exit 1
    fi
    echo -n "."
done
echo -e "\n${GREEN}Anvil 已就绪${NC}"

# 3. 环境配置
export DATABASE_URL="postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable"
export RPC_URLS="http://localhost:8545"
export CHAIN_ID="31337"
export START_BLOCK="0"
export EMULATOR_ENABLED="true"
export EMULATOR_RPC_URL="http://localhost:8545"
export EMULATOR_PRIVATE_KEY="ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
export EMULATOR_BLOCK_INTERVAL="1s"
export EMULATOR_TX_INTERVAL="5s"
export API_PORT="8080"
export LOG_LEVEL="info"
export LOG_FORMAT="text"
export CONTINUOUS_MODE="true"

# 4. 编译并运行
echo -e "${YELLOW}Step 4: 编译并启动 Indexer...${NC}"
mkdir -p bin
go build -o bin/indexer cmd/indexer/main.go

if [ $? -eq 0 ]; then
    echo -e "${GREEN}编译成功！服务启动中...${NC}"
    echo -e "${BLUE}Dashboard: http://localhost:8080${NC}"
    ./bin/indexer
else
    echo -e "${RED}编译失败，请检查代码错误${NC}"
    exit 1
fi