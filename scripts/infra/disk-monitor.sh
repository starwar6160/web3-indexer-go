#!/bin/bash
# 🚨 Web3 Indexer - Disk Space Monitoring
# Alerts when disk usage exceeds threshold and provides cleanup suggestions

set -e

PROJECT_DIR="/home/ubuntu/zwCode/web3-indexer-go"
LOG_FILE="$PROJECT_DIR/logs/disk-monitor.log"
ALERT_THRESHOLD=90
WARNING_THRESHOLD=80

# Create log directory if not exists
mkdir -p "$(dirname "$LOG_FILE")"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

check_disk_space() {
    local usage=$(df / | awk 'NR==2 {print $5}' | sed 's/%//')
    local available=$(df -h / | awk 'NR==2 {print $4}')

    log "💾 Disk usage: ${usage}% (${available} free)"

    if [ "$usage" -ge "$ALERT_THRESHOLD" ]; then
        log "🚨 CRITICAL: Disk usage at ${usage}%!"
        alert_critical "$usage" "$available"
        return 2
    elif [ "$usage" -ge "$WARNING_THRESHOLD" ]; then
        log "⚠️  WARNING: Disk usage at ${usage}%"
        alert_warning "$usage" "$available"
        return 1
    else
        log "✅ Disk usage within acceptable range"
        return 0
    fi
}

alert_critical() {
    local usage=$1
    local available=$2

    cat <<EOF
╔════════════════════════════════════════════════════════════╗
║         🚨 CRITICAL DISK SPACE ALERT 🚨                    ║
╠════════════════════════════════════════════════════════════╣
║  Current Usage: ${usage}%                                   ║
║  Available:    ${available}                                  ║
║                                                             ║
║  IMMEDIATE ACTION REQUIRED:                                 ║
║  1. Check Anvil container: docker exec web3-demo2-anvil du  ║
║  2. Run emergency cleanup: make anvil-emergency-cleanup     ║
║  3. Consider restarting Anvil: docker restart anvil         ║
╚════════════════════════════════════════════════════════════╝
EOF
}

alert_warning() {
    local usage=$1
    local available=$2

    cat <<EOF
⚠️  WARNING: Disk usage at ${usage}% (${available} free)
   Consider running: make check-disk-space
EOF
}

check_anvil_tmpfs() {
    # Try to find any running Anvil container
    local anvil_container=""
    if docker ps --format '{{.Names}}' | grep -q 'anvil'; then
        anvil_container=$(docker ps --format '{{.Names}}' | grep anvil | head -1)
        local tmpfs_percent=$(docker exec "$anvil_container" df /home/foundry/.foundry/anvil/tmp 2>/dev/null | awk 'NR==2 {print $5}' || echo "N/A")
        log "📏 Anvil tmpfs (${anvil_container}): ${tmpfs_percent} used"

        if [ "${tmpfs_percent%?}" -gt 80 ] 2>/dev/null; then
            log "⚠️  Anvil tmpfs approaching limit"
        fi
    else
        log "ℹ️  No Anvil container running"
    fi
}

# Main execution
log "════════════════════════════════════════════════════════════"
log "🔍 Starting disk space monitoring..."
check_disk_space
check_anvil_tmpfs
log "════════════════════════════════════════════════════════════"
exit $?
