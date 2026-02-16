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

log "✅ Maintenance complete. Anvil is fresh and Indexer is re-syncing."
