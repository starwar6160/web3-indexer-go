#!/bin/bash

# ============================================================================
# All-in-Docker Deployment Script for Web3 Indexer
# ============================================================================
# This script demonstrates the all-in-Docker architecture deployment
# Zero configuration, zero environment dependencies, one-click deployment

set -e

echo "🚀 Web3 Indexer - All-in-Docker Deployment"
echo "==========================================="
echo ""

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed. Please install Docker first.${NC}"
    exit 1
fi

if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}❌ Docker Compose is not installed. Please install Docker Compose first.${NC}"
    exit 1
fi

echo -e "${BLUE}📦 Architecture Topology:${NC}"
echo ""
echo "   ┌─────────────────────────────────────────┐"
echo "   │       Docker Compose Network            │"
echo "   │  ┌──────────┐  ┌──────────┐  ┌────────┐ │"
echo "   │  │    db    │  │  anvil   │  │indexer │ │"
echo "   │  │ :5432    │  │  :8545   │  │ :8080  │ │"
echo "   │  └──────────┘  └──────────┘  └────────┘ │"
echo "   └─────────────────────────────────────────┘"
echo ""

# Step 1: Clean up old containers
echo -e "${YELLOW}🔧 Step 1: Cleaning up old containers...${NC}"
docker compose down -v 2>/dev/null || true
echo -e "${GREEN}✅ Cleanup complete${NC}"
echo ""

# Step 2: Build and start services
echo -e "${YELLOW}🔧 Step 2: Building and starting all services...${NC}"
docker compose up -d --build
echo -e "${GREEN}✅ Services started${NC}"
echo ""

# Step 3: Wait for services to be ready
echo -e "${YELLOW}🔧 Step 3: Waiting for services to be ready...${NC}"
sleep 10

# Step 4: Verify services
echo -e "${YELLOW}🔧 Step 4: Verifying services...${NC}"
echo ""

echo -e "${BLUE}📊 Service Status:${NC}"
docker compose ps
echo ""

echo -e "${BLUE}🔍 Health Check:${NC}"
if curl -s http://localhost:8080/healthz 2>/dev/null | head -5; then
    echo -e "${GREEN}✅ Indexer is healthy${NC}"
else
    echo -e "${YELLOW}⚠️  Indexer is starting up...${NC}"
fi
echo ""

# Step 5: Display access information
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✅ All-in-Docker Deployment Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

echo -e "${BLUE}📍 Access Addresses:${NC}"
echo "   🎛️  Dashboard: http://localhost:8080"
echo "   ❤️   Health:   http://localhost:8080/healthz"
echo "   📊 Metrics:  http://localhost:8080/metrics"
echo ""

echo -e "${BLUE}🐳 Service Names (internal):${NC}"
echo "   Database:  db:5432"
echo "   Anvil RPC: anvil:8545"
echo "   Indexer:   indexer:8080"
echo ""

echo -e "${BLUE}💡 Useful Commands:${NC}"
echo "   View logs:   docker compose logs -f"
echo "   Stop all:    docker compose down"
echo "   Clean all:   docker compose down -v"
echo "   Restart:     docker compose restart"
echo ""

echo -e "${BLUE}📝 Configuration:${NC}"
echo "   All services use Docker service names for communication"
echo "   Database URL: postgres://postgres:postgres@db:5432/web3_indexer"
echo "   RPC URL:      http://anvil:8545"
echo "   Chain ID:     31337 (Anvil local chain)"
echo ""

echo -e "${YELLOW}💡 Next Steps:${NC}"
echo "   1. Monitor logs: docker compose logs -f indexer"
echo "   2. Check health: curl http://localhost:8080/healthz"
echo "   3. View dashboard: http://localhost:8080"
echo ""
