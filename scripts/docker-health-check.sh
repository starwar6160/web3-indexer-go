#!/bin/bash

# ============================================================================
# Docker Health Check Script
# ============================================================================
# Monitors the health of all services in the all-in-Docker deployment

set -e

echo "🏥 Web3 Indexer - Docker Health Check"
echo "======================================"
echo ""

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check Docker status
echo -e "${BLUE}📊 Docker Compose Status:${NC}"
docker compose ps
echo ""

# Check database connectivity
echo -e "${BLUE}🗄️  Database Health:${NC}"
if docker compose exec -T db pg_isready -U postgres &>/dev/null; then
    echo -e "${GREEN}✅ PostgreSQL is healthy${NC}"
else
    echo -e "${RED}❌ PostgreSQL is not responding${NC}"
fi
echo ""

# Check Anvil RPC
echo -e "${BLUE}⛽️  Anvil RPC Health:${NC}"
if docker compose exec -T anvil curl -s -X POST http://localhost:8545 \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' | grep -q 'result'; then
    echo -e "${GREEN}✅ Anvil RPC is responding${NC}"
else
    echo -e "${RED}❌ Anvil RPC is not responding${NC}"
fi
echo ""

# Check Indexer API
echo -e "${BLUE}🎛️  Indexer API Health:${NC}"
if curl -s http://localhost:8080/healthz &>/dev/null; then
    echo -e "${GREEN}✅ Indexer API is healthy${NC}"
    echo ""
    echo -e "${BLUE}Health Details:${NC}"
    curl -s http://localhost:8080/healthz | head -20
else
    echo -e "${RED}❌ Indexer API is not responding${NC}"
fi
echo ""

# Check network connectivity
echo -e "${BLUE}🌐 Network Connectivity:${NC}"
if docker compose exec -T indexer ping -c 1 db &>/dev/null; then
    echo -e "${GREEN}✅ Indexer can reach database${NC}"
else
    echo -e "${RED}❌ Indexer cannot reach database${NC}"
fi

if docker compose exec -T indexer curl -s http://anvil:8545 &>/dev/null; then
    echo -e "${GREEN}✅ Indexer can reach Anvil RPC${NC}"
else
    echo -e "${RED}❌ Indexer cannot reach Anvil RPC${NC}"
fi
echo ""

# Display resource usage
echo -e "${BLUE}💾 Resource Usage:${NC}"
docker compose stats --no-stream
echo ""

echo -e "${BLUE}📝 Service Logs (last 5 lines):${NC}"
echo ""
echo -e "${YELLOW}Database:${NC}"
docker compose logs --tail=5 db
echo ""
echo -e "${YELLOW}Anvil:${NC}"
docker compose logs --tail=5 anvil
echo ""
echo -e "${YELLOW}Indexer:${NC}"
docker compose logs --tail=5 indexer
