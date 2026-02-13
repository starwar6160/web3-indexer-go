#!/bin/bash

# ==============================================================================
# Web3 Indexer 数据库一键重置脚本 (仅针对 Indexer 业务数据)
# ==============================================================================

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== 正在重置 Web3 Indexer 业务数据 ===${NC}"

# 1. 确认目标数据库
DB_NAME="web3_indexer"
DB_USER="indexer_user"
DB_PASS="W3b3_Idx_Secur3_2026_Sec"

# 2. 执行 TRUNCATE (逻辑清理, 不伤及无辜)
echo -e "${YELLOW}正在清理表数据: blocks, transfers, sync_checkpoints...${NC}"

docker exec -e PGPASSWORD=$DB_PASS web3-indexer-db psql -U $DB_USER -d $DB_NAME -c "TRUNCATE TABLE blocks, transactions, transfers, logs, sync_checkpoints RESTART IDENTITY;"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ 业务数据已成功清空，序列已重置。${NC}"
else
    echo -e "${RED}❌ 重置失败，请检查数据库容器状态。${NC}"
    exit 1
fi

echo -e "${BLUE}提示: 此操作仅影响 $DB_NAME 数据库，Lobe-Chat 等其他数据库不受影响。${NC}"
