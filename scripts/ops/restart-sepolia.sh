#!/bin/bash

# Clean start script for Web3 Indexer on Sepolia Testnet
# This script ensures proper cleanup and idempotent startup

set -e  # Exit on any error

echo "🔄 Cleaning up previous testnet environment..."

# Use the docker compose directly for consistent cleanup
docker compose -f docker-compose.testnet.clean.yml -p web3-testnet down --remove-orphans || true

# Reset testnet database tables (preserving schema)
if docker compose -f docker-compose.testnet.clean.yml -p web3-testnet ps | grep -q sepolia-db; then
  echo "✅ Testnet database is running, resetting tables..."
  docker compose -f docker-compose.testnet.clean.yml -p web3-testnet exec sepolia-db psql -U postgres -d web3_sepolia -c "TRUNCATE TABLE blocks, transfers, sync_checkpoints RESTART IDENTITY;" 2>/dev/null || \
  echo "⚠️  Could not truncate tables (database may not be ready yet)"
else
  echo "⚠️  Testnet database container not running, skipping table reset"
fi

# Wait a moment for cleanup to complete
sleep 2

# Source the local environment file with your API keys
if [ -f ".env.testnet.local" ]; then
    echo "🔑 Loading API keys from .env.testnet.local..."
    export $(grep -v '^#' .env.testnet.local | xargs)
else
    echo "❌ Error: .env.testnet.local file not found"
    echo "💡 Please create .env.testnet.local with your API keys"
    exit 1
fi

# Verify that the required environment variable is set
if [ -z "$SEPOLIA_RPC_URLS" ]; then
    echo "❌ Error: SEPOLIA_RPC_URLS environment variable is not set"
    echo "💡 Check your .env.testnet.local file"
    exit 1
fi

# Start the fresh environment
echo "🚀 Starting fresh Web3 Indexer on Sepolia Testnet..."

# Run the a1 command which will start fresh
make a1

echo "✅ Web3 Indexer has been freshly started on Sepolia Testnet"
echo "🌐 Access the dashboard at: http://localhost:8081"
echo "📊 View metrics at: http://localhost:8081/metrics"
echo "📋 View logs with: make logs-testnet"
echo ""
echo "💡 Note: The indexer starts from the latest block to avoid historical data sync."
echo "   This means you'll see real-time blocks and transactions with minimal latency."