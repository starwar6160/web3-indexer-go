#!/bin/bash

# Dual Environment Management Script for Web3 Indexer
# This script helps manage both Anvil (local) and Sepolia (testnet) environments

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

show_help() {
    echo "Web3 Indexer Dual Environment Manager"
    echo ""
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  start-anvil     Start the Anvil (local development) environment"
    echo "  start-sepolia   Start the Sepolia (testnet) environment"
    echo "  start-both      Start both environments (requires SEPOLIA_RPC_URLS in env)"
    echo "  stop-all        Stop all environments"
    echo "  logs-anvil      Show logs for Anvil environment"
    echo "  logs-sepolia    Show logs for Sepolia environment"
    echo "  status          Show status of all containers"
    echo "  clean           Remove all containers and volumes"
    echo "  help            Show this help message"
    echo ""
    echo "Environment Variables:"
    echo "  SEPOLIA_RPC_URLS  Comma-separated list of Sepolia RPC URLs (required for Sepolia env)"
    echo ""
}

start_anvil() {
    echo "🚀 Starting Anvil (local development) environment..."
    docker-compose -f docker-compose.yml up -d db anvil indexer
    echo "✅ Anvil environment started!"
    echo "   - Anvil RPC: http://localhost:8545"
    echo "   - Indexer UI: http://localhost:8080"
    echo "   - Database: postgres://localhost:15432/web3_indexer"
}

start_sepolia() {
    if [ -z "$SEPOLIA_RPC_URLS" ]; then
        echo "❌ Error: SEPOLIA_RPC_URLS environment variable is required for Sepolia environment"
        echo "Please set it to your Sepolia RPC URLs (comma-separated if multiple)"
        echo "Example: export SEPOLIA_RPC_URLS='https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY,https://sepolia.infura.io/v3/YOUR_KEY'"
        exit 1
    fi
    
    echo "🚀 Starting Sepolia (testnet) environment..."
    docker-compose -f docker-compose.testnet.yml up -d sepolia-db sepolia-indexer
    echo "✅ Sepolia environment started!"
    echo "   - Indexer UI: http://localhost:8081"
    echo "   - Database: postgres://localhost:15433/web3_sepolia"
}

start_both() {
    if [ -z "$SEPOLIA_RPC_URLS" ]; then
        echo "❌ Error: SEPOLIA_RPC_URLS environment variable is required for Sepolia environment"
        echo "Please set it to your Sepolia RPC URLs (comma-separated if multiple)"
        echo "Example: export SEPOLIA_RPC_URLS='https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY,https://sepolia.infura.io/v3/YOUR_KEY'"
        exit 1
    fi
    
    echo "🚀 Starting both Anvil and Sepolia environments..."
    docker-compose -f docker-compose.yml up -d db anvil indexer
    docker-compose -f docker-compose.testnet.yml up -d sepolia-db sepolia-indexer
    echo "✅ Both environments started!"
    echo "   Anvil:"
    echo "   - RPC: http://localhost:8545"
    echo "   - Indexer UI: http://localhost:8080"
    echo "   - Database: postgres://localhost:15432/web3_indexer"
    echo ""
    echo "   Sepolia:"
    echo "   - Indexer UI: http://localhost:8081"
    echo "   - Database: postgres://localhost:15433/web3_sepolia"
}

stop_all() {
    echo "🛑 Stopping all environments..."
    docker-compose -f docker-compose.yml down
    docker-compose -f docker-compose.testnet.yml down
    echo "✅ All environments stopped!"
}

logs_anvil() {
    echo "📋 Anvil environment logs:"
    docker-compose -f docker-compose.yml logs -f indexer
}

logs_sepolia() {
    echo "📋 Sepolia environment logs:"
    docker-compose -f docker-compose.testnet.yml logs -f sepolia-indexer
}

status() {
    echo "📊 Container Status:"
    docker ps -a --filter "name=web3-indexer" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
}

clean() {
    echo "🗑️ Removing all containers and volumes..."
    docker-compose -f docker-compose.yml down -v
    docker-compose -f docker-compose.testnet.yml down -v
    echo "✅ Cleanup complete!"
}

case "${1:-help}" in
    start-anvil)
        start_anvil
        ;;
    start-sepolia)
        start_sepolia
        ;;
    start-both)
        start_both
        ;;
    stop-all)
        stop_all
        ;;
    logs-anvil)
        logs_anvil
        ;;
    logs-sepolia)
        logs_sepolia
        ;;
    status)
        status
        ;;
    clean)
        clean
        ;;
    help|"")
        show_help
        ;;
    *)
        echo "Unknown command: $1"
        echo ""
        show_help
        exit 1
        ;;
esac