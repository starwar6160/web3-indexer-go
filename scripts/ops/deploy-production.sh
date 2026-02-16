#!/bin/bash

# Web3 Indexer Production Deployment Script
# Targets: Docker Host Mode for Maximized Performance
# 2026-02-15

set -e

GREEN='\033[0;32m'
NC='\033[0m'

echo -e "${GREEN}🚀 Starting Production Infrastructure Deployment...${NC}"

# 1. Ensure any conflicting bridge-mode services are down
docker-compose -f docker-compose.infra.yml down --remove-orphans

# 2. Launch Infrastructure in Host Mode
# This includes Postgres (15432), Prometheus (9091), and Grafana (4000)
docker-compose -f docker-compose.infra.yml up -d

echo -e "${GREEN}✅ Infrastructure is UP in Host Mode.${NC}"

# 3. Verify Ports
echo "🔍 Verifying ports..."
./check_ports.sh

# 4. Generate Production Startup Command
echo "--------------------------------------------------------"
echo -e "${GREEN}Final Step: Start your Indexer in background mode:${NC}"
echo ""
echo "  nohup go run ./cmd/indexer --start-from latest > logs/indexer_prod.log 2>&1 &"
echo ""
echo "Or use make a1 if you have configured docker-compose.yml for it."
echo "Visit Dashboard: http://localhost:4000"
echo "--------------------------------------------------------"
