#!/bin/bash

# Start the Web3 Indexer on Sepolia Testnet with your API keys
# This script ensures proper environment variable assignment without line break issues

echo "🚀 Starting Web3 Indexer on Sepolia Testnet..."

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

# Run the docker compose directly
export $(grep -v '^#' .env.testnet.local | xargs)
docker compose -f docker-compose.testnet.clean.yml -p web3-testnet up -d --build

echo "✅ Web3 Indexer should now be running on Sepolia Testnet"
echo "🌐 Access the dashboard at: http://localhost:8081"
echo "📊 View metrics at: http://localhost:8081/metrics"
echo "📋 View logs with: make logs-testnet"
echo ""
echo "💡 Note: The indexer starts from the latest block to avoid historical data sync."
echo "   This means you'll see real-time blocks and transactions with minimal latency."