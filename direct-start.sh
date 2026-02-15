#!/bin/bash

# Direct start script for Web3 Indexer on Sepolia Testnet
# Loads environment and starts everything directly

set -e  # Exit on any error

echo "🔄 Cleaning up previous testnet environment..."

# Stop any existing testnet containers
docker compose -f docker-compose.testnet.yml -p web3-testnet down --remove-orphans || true

# Wait a moment for cleanup to complete
sleep 2

# Source the local environment file with your API keys
if [ -f ".env.testnet.local" ]; then
    echo "🔑 Loading API keys from .env.testnet.local..."
    # Load the environment variables
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

# Start the fresh environment using docker compose directly with environment
echo "🚀 Starting fresh Web3 Indexer on Sepolia Testnet..."

# Export the variables and run docker compose
docker compose -f docker-compose.testnet.clean.yml -p web3-testnet up -d --build

echo "✅ Web3 Indexer has been started on Sepolia Testnet"
echo "🌐 Access the dashboard at: http://localhost:8081"
echo "📊 View metrics at: http://localhost:8081/metrics"
echo "📋 View logs with: docker compose -f docker-compose.testnet.yml -p web3-testnet logs -f sepolia-indexer"
echo ""
echo "💡 Note: The indexer starts from the latest block to avoid historical data sync."
echo "   This means you'll see real-time blocks and transactions with minimal latency."