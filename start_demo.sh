#!/bin/bash

# ==============================================================================
# Web3 Indexer 全自动演示流水线
# ==============================================================================

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== 启动 Web3 Indexer 工业级演示流水线 ===${NC}"

# 1. 重置环境
echo -e "${YELLOW}Step 1: 物理大扫除 (清空数据库与数据卷)...${NC}"
./scripts/clean-env.sh

# 2. 启动 Anvil (强制开启自动产块)
echo -e "${YELLOW}Step 2: 启动本地 Anvil (Block-time: 1s)...${NC}"
docker compose -f docker-compose.infra.yml up -d anvil

# 3. 编译并启动索引器
echo -e "${YELLOW}Step 3: 启动热重载索引器 (物理锁定模式)...${NC}"
echo -e "${GREEN}提示：索引器将自动从区块 0 开始同步。${NC}"

# 使用后台运行 air，这样脚本可以继续打印提示
export PATH=$PATH:$(go env GOPATH)/bin
air &
AIR_PID=$!

# 4. 开启自动高频压测

echo -e "${YELLOW}Step 4: 开启自动高频压测 (TPS: ~50)...${NC}"

python3 scripts/stress-test.py > /dev/null 2>&1 &

STRESS_PID=$!



# 5. 引导操作

echo -e "\n${BLUE}=== 演示就绪！ ===${NC}"

echo -e "1. 浏览器访问: ${GREEN}http://localhost:8080${NC}"

echo -e "2. 观察实时变化: 正在自动产生区块和高频交易。"

echo -e "3. 停止演示: 请按 ${RED}Ctrl+C${NC} 停止此脚本。"



# 等待信号

trap "kill $AIR_PID $STRESS_PID; exit" SIGINT SIGTERM

wait $AIR_PID
