#!/bin/bash

# ============================================================================
# Docker Shell Access Script
# ============================================================================
# Provides easy shell access to running containers

set -e

echo "🐚 Web3 Indexer - Docker Shell Access"
echo "====================================="
echo ""

SERVICE=${1:-""}

if [ -z "$SERVICE" ]; then
    echo "Usage: $0 [service]"
    echo ""
    echo "Available services:"
    echo "  db       - PostgreSQL database shell"
    echo "  anvil    - Anvil RPC container shell"
    echo "  indexer  - Indexer application shell"
    echo ""
    echo "Examples:"
    echo "  $0 db       - Access PostgreSQL psql"
    echo "  $0 indexer  - Access indexer container"
    echo ""
    exit 0
fi

case $SERVICE in
    db)
        echo "Connecting to PostgreSQL..."
        docker compose exec db psql -U postgres -d web3_indexer
        ;;
    anvil)
        echo "Accessing Anvil container..."
        docker compose exec anvil sh
        ;;
    indexer)
        echo "Accessing Indexer container..."
        docker compose exec indexer sh
        ;;
    *)
        echo "Unknown service: $SERVICE"
        exit 1
        ;;
esac
