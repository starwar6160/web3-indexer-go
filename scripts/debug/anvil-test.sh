#!/bin/bash

set -e

echo "🚀 Web3 Indexer - Anvil Testing Workflow"
echo "=========================================="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Step 1: Start Anvil and PostgreSQL
echo -e "${BLUE}Step 1: Starting Anvil and PostgreSQL...${NC}"
docker compose -f docker-compose.infra.yml --profile testing up -d postgres anvil
sleep 3

# Verify Anvil is running
echo -e "${BLUE}Verifying Anvil connection...${NC}"
if ! curl -s http://localhost:8545 -X POST -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' > /dev/null; then
    echo -e "${RED}❌ Anvil failed to start${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Anvil is running${NC}"

# Step 2: Build the indexer
echo ""
echo -e "${BLUE}Step 2: Building indexer binary...${NC}"
mkdir -p bin
go build -o bin/indexer ./cmd/indexer
echo -e "${GREEN}✅ Build complete${NC}"

# Step 3: Deploy demo contracts and transactions
echo ""
echo -e "${BLUE}Step 3: Deploying demo contracts and sending test transactions...${NC}"
RPC_URL=http://localhost:8545 go run ./cmd/demo/deploy.go
echo -e "${GREEN}✅ Demo deployment complete${NC}"

# Step 4: Start the indexer
echo ""
echo -e "${BLUE}Step 4: Starting indexer with Anvil...${NC}"
echo -e "${YELLOW}Configuration:${NC}"
echo "  RPC_URLS: http://localhost:8545"
echo "  CHAIN_ID: 31337 (Anvil)"
echo "  START_BLOCK: 0"
echo "  DATABASE_URL: postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable"
echo ""

# Run indexer with timeout to allow observation
export DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable
export RPC_URLS=http://localhost:8545
export CHAIN_ID=31337
export START_BLOCK=0
export LOG_LEVEL=debug

timeout 60 ./bin/indexer || true

echo ""
echo -e "${GREEN}✅ Indexer test run complete${NC}"

# Step 5: Cleanup
echo ""
echo -e "${BLUE}Step 5: Cleaning up...${NC}"
docker compose -f docker-compose.infra.yml --profile testing down
echo -e "${GREEN}✅ Cleanup complete${NC}"

echo ""
echo -e "${GREEN}✨ Anvil testing workflow finished!${NC}"
