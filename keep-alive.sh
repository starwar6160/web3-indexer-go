#!/bin/bash
# 🏥 Web3 Indexer Watchdog - SRE High Availability Script
# This script monitors ports 8081, 8082, 8083 and restarts containers if offline.

LOG_FILE="/home/ubuntu/zwCode/web3-indexer-go/logs/watchdog.log"
PROJECT_DIR="/home/ubuntu/zwCode/web3-indexer-go"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE"
}

check_port() {
    local port=$1
    if curl -s "http://localhost:$port/api/status" > /dev/null; then
        return 0 # Healthy
    else
        return 1 # Offline
    fi
}

cd "$PROJECT_DIR" || exit 1

while true; do
    # 1. Check Port 8082 (Demo2 - High Priority)
    if ! check_port 8082; then
        log "🚨 ALERT: Port 8082 (Demo2) is OFFLINE. Restarting..."
        COMPOSE_PROJECT_NAME=web3-demo2 docker-compose --env-file .env.demo2 up -d indexer >> "$LOG_FILE" 2>&1
    fi

    # 2. Check Port 8081 (Sepolia - High Priority)
    if ! check_port 8081; then
        log "🚨 ALERT: Port 8081 (Sepolia) is OFFLINE. Restarting..."
        docker-compose -f docker-compose.testnet.yml --env-file .env.testnet up -d sepolia-indexer >> "$LOG_FILE" 2>&1
    fi

    # 3. Check Port 8083 (Debug)
    if ! check_port 8083; then
        log "⚠️  NOTICE: Port 8083 (Debug) is OFFLINE. Restarting..."
        docker-compose -f docker-compose.debug.yml --env-file .env.debug.commercial up -d debug-indexer >> "$LOG_FILE" 2>&1
    fi

    sleep 30
done
