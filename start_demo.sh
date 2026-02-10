#!/bin/bash

# ==============================================================================
# Web3 Indexer 全自动演示流水线 (Docker 全栈版)
# ==============================================================================

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== 启动 Web3 Indexer 工业级演示流水线 (Docker-Only) ===${NC}"

# 1. 环境清理
echo -e "${YELLOW}Step 1: 物理大扫除 (清理旧容器、卷与孤立网络)...${NC}"
docker compose down -v --remove-orphans

# 2. 启动全栈环境
echo -e "${YELLOW}Step 2: 启动全栈环境 (DB + Anvil + Indexer)...${NC}"
docker compose up --build -d

# 2.5 激活链上数据 (触发首笔测试转账)
echo -e "${YELLOW}Step 2.5: 激活链上数据 (触发首笔测试转账)...${NC}"
# 确保 Anvil 已准备好接收请求
MAX_RETRIES=10
COUNT=0
while ! docker exec web3-indexer-anvil cast block-number > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    COUNT=$((COUNT + 1))
    if [ $COUNT -ge $MAX_RETRIES ]; then
        echo -e "\n${RED}❌ Anvil 响应超时${NC}"
        break
    fi
done

if [ $COUNT -lt $MAX_RETRIES ]; then
    # 执行测试转账：从 Anvil 默认账户 #0 发送 0.1 ETH 到账户 #1
    docker exec web3-indexer-anvil cast send \
      --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
      --value 0.1ether \
      0x70997970C51812dc3A010C7d01b50e0d17dc79C8 \
      --rpc-url http://127.0.0.1:8545 > /dev/null 2>&1
    echo -e "\n${GREEN}✅ 首笔交易已上链，Indexer 将在 1s 内同步。${NC}"
fi

# 3. 等待服务就绪
echo -e "${YELLOW}Step 3: 等待 API 服务就绪...${NC}"
until curl -s http://localhost:8080/healthz > /dev/null; do
    echo -n "."
    sleep 2
done
echo -e "\n${GREEN}API 已就绪!${NC}"

# 4. 开启自动高频压测 (本地运行，因为 VM 已安装 Python)
echo -e "${YELLOW}Step 4: 开启自动高频压测 (TPS: ~50)...${NC}"
# 确保安装了必要的 python 库 (如果有 requirements.txt)
# pip install -r scripts/requirements.txt > /dev/null 2>&1 
python3 scripts/stress-test.py > /dev/null 2>&1 &
STRESS_PID=$!

# 5. 引导操作
echo -e "\n${BLUE}=== 演示就绪！ ===${NC}"
echo -e "1. 浏览器访问: ${GREEN}http://localhost:8080${NC}"
echo -e "2. 观察实时变化: Docker 容器正在处理高频交易。"
echo -e "3. 停止演示: 请按 ${RED}Ctrl+C${NC} (将清理容器)。"

# 等待信号
trap "kill $STRESS_PID; docker compose down; exit" SIGINT SIGTERM

# 持续观察日志
docker compose logs -f indexer