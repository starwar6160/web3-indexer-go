#!/bin/bash

# ============================================================================
# Docker Logs Viewer Script
# ============================================================================
# Provides easy access to service logs with filtering options

set -e

echo "📋 Web3 Indexer - Docker Logs Viewer"
echo "===================================="
echo ""

# Color codes
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default service
SERVICE=${1:-""}

if [ -z "$SERVICE" ]; then
    echo "Usage: $0 [service] [options]"
    echo ""
    echo "Services:"
    echo "  all      - View logs from all services"
    echo "  db       - View database logs"
    echo "  anvil    - View Anvil RPC logs"
    echo "  indexer  - View indexer logs"
    echo ""
    echo "Options:"
    echo "  -f, --follow   - Follow log output (default)"
    echo "  -n, --lines N  - Show last N lines"
    echo ""
    echo "Examples:"
    echo "  $0 all              - Follow all service logs"
    echo "  $0 indexer -n 50    - Show last 50 lines of indexer logs"
    echo "  $0 db -f            - Follow database logs"
    echo ""
    exit 0
fi

# Parse options
FOLLOW="-f"
LINES=""

shift
while [[ $# -gt 0 ]]; do
    case $1 in
        -f|--follow)
            FOLLOW="-f"
            shift
            ;;
        -n|--lines)
            LINES="--tail=$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done

# View logs
case $SERVICE in
    all)
        echo -e "${BLUE}📋 All Services Logs:${NC}"
        docker compose logs $FOLLOW $LINES
        ;;
    db)
        echo -e "${BLUE}🗄️  Database Logs:${NC}"
        docker compose logs $FOLLOW $LINES db
        ;;
    anvil)
        echo -e "${BLUE}⛽️  Anvil RPC Logs:${NC}"
        docker compose logs $FOLLOW $LINES anvil
        ;;
    indexer)
        echo -e "${BLUE}🎛️  Indexer Logs:${NC}"
        docker compose logs $FOLLOW $LINES indexer
        ;;
    *)
        echo "Unknown service: $SERVICE"
        exit 1
        ;;
esac
