#!/bin/bash

# ==============================================================================
# Web3 Indexer 工业级修复版开发脚本 (V2)
# ==============================================================================

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Web3 Indexer 工业级环境重置中 ===${NC}"

# 1. 彻底杀死现有索引器进程 (确保编译不受干扰)
pkill -f "indexer" 2>/dev/null || true

# 2. 深度清理容器和数据卷 (确保数据库重新创建)
docker compose -f docker-compose.infra.yml --profile testing down -v 2>/dev/null || true
docker volume rm web3-indexer-go_indexer_db_data web3-indexer-go_indexer_anvil_data 2>/dev/null || true

# 3. 启动基础设施
docker compose -f docker-compose.infra.yml --profile testing up -d postgres anvil

# 4. 鲁棒健康检查
echo -e "${YELLOW}等待基础设施就绪...${NC}"

# A. 等待 Postgres 真正的健康状态
until docker exec web3-indexer-db pg_isready -U postgres -d web3_indexer > /dev/null 2>&1; do
    echo -n "P"
    sleep 1
done
echo -e "\n${GREEN}Postgres 已就绪 (DB: web3_indexer)${NC}"

# B. 等待 Anvil RPC 响应
echo -e "${YELLOW}等待 Anvil (8545) 响应...${NC}"
MAX_RETRIES=30
COUNT=0
# 使用 network_mode: host，所以直接 curl localhost
until curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' http://localhost:8545 | grep -q "result" > /dev/null 2>&1; do
    echo -n "A"
    sleep 1
    COUNT=$((COUNT + 1))
    if [ $COUNT -ge $MAX_RETRIES ]; then
        echo -e "\n${RED}错误: Anvil 启动超时。尝试手动检查 docker logs web3-indexer-anvil${NC}"
        exit 1
    fi
done
echo -e "\n${GREEN}Anvil 已就绪${NC}"

# --- 数据库 Schema 幂等补全 (工业级防御) ---
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

# 5. 编译并启动
echo -e "${YELLOW}正在编译 Indexer...${NC}"
mkdir -p bin
go build -o bin/indexer cmd/indexer/main.go
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ 编译失败，请检查 main.go 代码${NC}"
    exit 1
fi

export DATABASE_URL="postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable"
export RPC_URLS="http://localhost:8545"
export CHAIN_ID="31337"
export START_BLOCK="0"
export EMULATOR_ENABLED="true"
export EMULATOR_RPC_URL="http://localhost:8545"
export EMULATOR_PRIVATE_KEY="ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
export LOG_LEVEL="info"
export CONTINUOUS_MODE="true"

echo -e "${GREEN}🚀 服务启动中！访问 Dashboard: http://localhost:8080${NC}"
./bin/indexer