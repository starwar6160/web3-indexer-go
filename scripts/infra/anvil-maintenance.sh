#!/bin/bash
# 🧹 Web3 Indexer - Anvil Maintenance & State Cleanup
# Prevents memory bloat and disk I/O accumulation from local blockchain simulation.

LOG_FILE="/home/ubuntu/zwCode/web3-indexer-go/logs/maintenance.log"
PROJECT_DIR="/home/ubuntu/zwCode/web3-indexer-go"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE"
}

cd "$PROJECT_DIR" || exit 1

log "🚀 Starting Anvil scheduled maintenance..."

# 1. 重启 Anvil 容器 (这会清空内存中的状态)
log "♻️  Restarting web3-demo2-anvil to clear memory bloat..."
COMPOSE_PROJECT_NAME=web3-demo2 docker-compose restart anvil >> "$LOG_FILE" 2>&1

# 2. 清理 Indexer 的 Demo 数据库 (由于 Anvil 重置了，数据库也必须对齐)
log "🧹 Cleaning web3_demo database to align with new chain genesis..."
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h localhost -p 15432 -U postgres -d web3_demo -c "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;" >> "$LOG_FILE" 2>&1

# 3. 重启 Indexer 容器以重新开始同步
log "🔄 Relaunching web3-demo2-app..."
COMPOSE_PROJECT_NAME=web3-demo2 docker-compose up -d indexer >> "$LOG_FILE" 2>&1

# 4. Verify tmpfs usage is within limits
ANVIL_CONTAINER=$(docker ps --format '{{.Names}}' | grep anvil | head -1 || echo "")
if [ -n "$ANVIL_CONTAINER" ]; then
    TMPFS_USAGE=$(docker exec "$ANVIL_CONTAINER" du -sh /home/foundry/.foundry/anvil/tmp 2>/dev/null | cut -f1 || echo "0")
    log "📏 Anvil tmpfs usage: ${TMPFS_USAGE}"

    # 5. Alert if approaching tmpfs limit (80% warning)
    TMPFS_PERCENT=$(docker exec "$ANVIL_CONTAINER" df /home/foundry/.foundry/anvil/tmp 2>/dev/null | awk 'NR==2 {print $5}' | sed 's/%//' || echo "0")
    if [ "$TMPFS_PERCENT" -gt 80 ]; then
        log "⚠️  WARNING: tmpfs usage at ${TMPFS_PERCENT}%"
    fi
fi

log "✅ Maintenance complete. Anvil is fresh and Indexer is re-syncing."
