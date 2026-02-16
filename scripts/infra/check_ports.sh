#!/bin/bash

# Web3 Indexer Port Checker (Host Mode)
# 2026-02-15

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

echo "🔍 Checking infrastructure ports on host network..."

check_port() {
    local port=$1
    local name=$2
    if netstat -tunlp | grep ":$port " > /dev/null; then
        echo -e "[${GREEN}OK${NC}] $name is listening on port $port"
    else
        echo -e "[${RED}FAIL${NC}] $name is NOT listening on port $port"
    fi
}

# 1. Database (Postgres)
check_port 15432 "PostgreSQL (Infrastructure)"

# 2. Prometheus
check_port 9091 "Prometheus (Monitoring)"

# 3. Grafana
check_port 4000 "Grafana (UI)"

# 4. Indexer Process (go run)
check_port 8081 "Indexer B1 (API/Metrics)"

echo "---------------------------------------"
echo "If all [OK], your monitoring link should be active."
echo "Visit Grafana at http://localhost:4000 (or your Tailscale IP:4000)"
