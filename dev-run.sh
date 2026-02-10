#!/bin/bash

# ==============================================================================
# Web3 Indexer 工业级开发运行脚本 (V3 - 编译优先版)
# ==============================================================================

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Web3 Indexer 开发环境启动流程 ===${NC}"

# 1. 首先编译确定正确性 (Fail-fast 原则)
echo -e "${YELLOW}Step 1: 正在进行代码预编译检查...${NC}"
mkdir -p bin
go build -o bin/indexer cmd/indexer/main.go
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ 编译失败！请先修复代码错误后再运行脚本。${NC}"
    exit 1
fi
echo -e "${GREEN}✅ 代码预检通过${NC}"

# 2. 检查是否需要重置基础设施
RESET_FLAG=false
if [[ "$1" == "--reset" ]] || [[ "$1" == "-r" ]]; then
    RESET_FLAG=true
fi

if [ "$RESET_FLAG" = true ]; then
    echo -e "${RED}Step 2: [!!!] 正在执行深度重置 (物理清理数据卷)...${NC}"
    pkill -f "indexer" 2>/dev/null || true
    docker compose -f docker-compose.infra.yml --profile testing down -v 2>/dev/null || true
    docker volume rm web3-indexer-go_indexer_db_data web3-indexer-go_indexer_anvil_data 2>/dev/null || true
    echo -e "${GREEN}✅ 物理环境已恢复至原始状态${NC}"
else
    echo -e "${BLUE}Step 2: 正在复用现有基础设施环境 (跳过重置, 使用 --reset 执行彻底清理)${NC}"
    pkill -f "indexer" 2>/dev/null || true
fi

# 3. 启动基础设施
echo -e "${YELLOW}Step 3: 确保 Docker 基础设施 (Postgres + Anvil) 运行中...${NC}"
docker compose -f docker-compose.infra.yml --profile testing up -d postgres anvil

# 4. 鲁棒健康检查
echo -e "${YELLOW}等待基础设施就绪...${NC}"

# A. 等待 Postgres 真正的健康状态
until docker exec web3-indexer-db pg_isready -U postgres -d web3_indexer > /dev/null 2>&1; do
    echo -n "P"
    sleep 1
done
echo -e "\n${GREEN}Postgres 已就绪${NC}"

# B. 等待 Anvil RPC 响应
until curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' http://localhost:8545 | grep -q "result" > /dev/null 2>&1; do
    echo -n "A"
    sleep 1
done
echo -e "\n${GREEN}Anvil 已就绪${NC}"

# --- 数据库 Schema 幂等补全 ---
echo -e "${YELLOW}正在验证数据库 Schema...${NC}"
docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
    ALTER TABLE blocks ADD COLUMN IF NOT EXISTS parent_hash VARCHAR(66) NOT NULL DEFAULT '';
    ALTER TABLE transfers ADD COLUMN IF NOT EXISTS tx_hash CHAR(66) NOT NULL DEFAULT '';
    ALTER TABLE transfers ALTER COLUMN amount TYPE NUMERIC(78,0);
    DO \$\$ 
    BEGIN 
        IF (SELECT data_type FROM information_schema.columns WHERE table_name = 'blocks' AND column_name = 'timestamp') = 'timestamp without time zone' THEN
            ALTER TABLE blocks ALTER COLUMN timestamp TYPE BIGINT USING EXTRACT(EPOCH FROM timestamp)::BIGINT;
        END IF;
    END \$\$;" > /dev/null 2>&1
echo -e "${GREEN}Schema 验证完成${NC}"

# 5. 最终启动
export DATABASE_URL="postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable"
export RPC_URLS="http://localhost:8545"
export CHAIN_ID="31337"
export START_BLOCK="0"
export EMULATOR_ENABLED="true"
export EMULATOR_RPC_URL="http://localhost:8545"
export EMULATOR_PRIVATE_KEY="ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
export LOG_LEVEL="info"
export CONTINUOUS_MODE="true"

echo -e "${GREEN}🚀 工业级引擎启动中！访问 Dashboard: http://localhost:8080${NC}"
./bin/indexer
