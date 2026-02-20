#!/bin/sh
# entrypoint-wrapper.sh - Docker entrypoint wrapper for force_beast alignment
set -e

# Run force_beast alignment before starting indexer (if enabled)
if [ "${FORCE_BEAST_ALIGNMENT:-true}" = "true" ] && [ -f "/app/scripts/force_beast.sh" ]; then
    echo "[entrypoint] Running force_beast alignment..."
    # Export required env vars for the script
    export ROOT_DIR=/app
    export ENV_FILE=${ENV_FILE:-/app/.env}
    export CLEAR_CHECKPOINTS=${CLEAR_CHECKPOINTS_ON_START:-true}
    
    # Run alignment (non-fatal if it fails, we'll still start)
    if /app/scripts/force_beast.sh 2>/dev/null; then
        echo "[entrypoint] ✓ Force beast alignment completed"
    else
        echo "[entrypoint] ⚠ Force beast alignment skipped or failed, continuing..."
    fi
else
    echo "[entrypoint] Skipping force_beast alignment (disabled or script not found)"
fi

# Source updated env file if it exists
if [ -f "/app/.env" ]; then
    # Only export vars that are not already set
    set -a
    . /app/.env 2>/dev/null || true
    set +a
fi

# Start the actual indexer
echo "[entrypoint] Starting indexer..."
exec ./indexer "$@"
