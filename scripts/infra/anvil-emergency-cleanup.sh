#!/bin/bash
# 🚨 Web3 Indexer - Anvil Emergency Disk Cleanup
# Safely frees disk space by cleaning Anvil container without affecting indexer data

set -e

PROJECT_DIR="/home/ubuntu/zwCode/web3-indexer-go"
LOG_FILE="$PROJECT_DIR/logs/emergency-cleanup.log"

# Create log directory if not exists
mkdir -p "$(dirname "$LOG_FILE")"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

error() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ❌ ERROR: $1" | tee -a "$LOG_FILE" >&2
    exit 1
}

# Pre-cleanup snapshot
log "📊 Taking pre-cleanup snapshot..."
df -h > /tmp/disk-before-cleanup.log
docker ps > /tmp/containers-before-cleanup.log

# Verify testnet is not affected
if docker ps | grep -q web3-testnet-app; then
    log "✅ Testnet container detected - will be preserved"
fi

# Find Anvil container name (could be web3-demo2-anvil or similar)
ANVIL_CONTAINER=$(docker ps --format '{{.Names}}' | grep anvil | head -1 || echo "")

if [ -z "$ANVIL_CONTAINER" ]; then
    log "⚠️  No Anvil container found running. Checking for stopped containers..."
    ANVIL_CONTAINER=$(docker ps -a --format '{{.Names}}' | grep anvil | head -1 || echo "")

    if [ -z "$ANVIL_CONTAINER" ]; then
        log "ℹ️  No Anvil container found. Nothing to clean up."
        exit 0
    fi
fi

log "🎯 Target Anvil container: ${ANVIL_CONTAINER}"

# Stop Anvil container
log "🛑 Stopping ${ANVIL_CONTAINER} container..."
if docker stop "$ANVIL_CONTAINER"; then
    log "✅ Container stopped successfully"
else
    error "Failed to stop container"
fi

# Remove Anvil container (frees overlay2 space)
log "🗑️  Removing ${ANVIL_CONTAINER} container..."
if docker rm "$ANVIL_CONTAINER"; then
    log "✅ Container removed successfully"
else
    error "Failed to remove container"
fi

# Clean up dangling images
log "🧹 Cleaning up dangling Docker resources..."
docker image prune -f >> "$LOG_FILE" 2>&1
docker container prune -f >> "$LOG_FILE" 2>&1

# Post-cleanup verification
log "📊 Post-cleanup disk space:"
df -h | tee -a "$LOG_FILE"

# Restart Anvil with safe defaults
log "🔄 Restarting Anvil container..."
cd "$PROJECT_DIR"

# Detect COMPOSE_PROJECT_NAME from container name pattern
if [[ "$ANVIL_CONTAINER" == *"demo2"* ]]; then
    export COMPOSE_PROJECT_NAME=web3-demo2
elif [[ "$ANVIL_CONTAINER" == *"demo"* ]]; then
    export COMPOSE_PROJECT_NAME=web3-demo
else
    export COMPOSE_PROJECT_NAME=web3-indexer
fi

log "📦 Using COMPOSE_PROJECT_NAME: ${COMPOSE_PROJECT_NAME}"

if docker-compose -f configs/docker/docker-compose.yml up -d anvil; then
    log "✅ Anvil restarted successfully"
else
    error "Failed to restart Anvil"
fi

# Get new container name
NEW_CONTAINER=$(docker ps --format '{{.Names}}' | grep anvil | head -1 || echo "")
log "📦 New container name: ${NEW_CONTAINER}"

# Verify Anvil is responsive
sleep 3
if curl -sf http://localhost:8545 > /dev/null; then
    log "✅ Anvil is responding on port 8545"
else
    log "⚠️  Warning: Anvil may not be fully initialized yet"
fi

log "🎉 Emergency cleanup completed successfully!"
log "📋 Logs saved to: ${LOG_FILE}"
