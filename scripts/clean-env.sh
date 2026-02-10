#!/bin/bash

# ==============================================================================
# Web3 Indexer 环境重置脚本 (演示专用)
# ==============================================================================

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}正在执行环境重置流程...${NC}"

# 1. 停止本地索引器进程 (如果正在运行)
echo -e "Step 1: 停止正在运行的索引器..."
pkill indexer || true

# 2. 重置 Docker 基础设施并清除 Volumes
echo -e "Step 2: 重置 Docker 容器并清空数据卷..."
docker compose -f docker-compose.infra.yml down -v
docker compose -f docker-compose.infra.yml up -d

# 3. 使用索引器的 --reset 标志启动一次以清理表结构 (确保 DB 已就绪)
echo -e "Step 3: 执行数据库逻辑清理..."
# 给数据库一点启动时间
sleep 3
./bin/indexer --reset &
PID=$!
sleep 2
kill $PID

echo -e "${GREEN}✅ 环境已重置！${NC}"
echo -e "${YELLOW}提示：现在可以执行 sudo systemctl restart web3-indexer 开启全新的同步演示。${NC}"
