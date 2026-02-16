#!/bin/bash
# 🚀 Web3 Indexer - Near Zero Downtime Deployment Script
# Strategy: Build first in background, then quick-swap container.

SERVICE_NAME=$1
ENV_FILE=$2
PROJECT_NAME=$3

if [ -z "$SERVICE_NAME" ] || [ -z "$ENV_FILE" ]; then
    echo "Usage: ./deploy-fast.sh <service> <env-file> [project-name]"
    exit 1
fi

echo "📦 [1/3] Pre-building image for $SERVICE_NAME..."
if [ -n "$PROJECT_NAME" ]; then
    COMPOSE_PROJECT_NAME=$PROJECT_NAME docker-compose --env-file "$ENV_FILE" build "$SERVICE_NAME"
else
    docker-compose -f "$SERVICE_NAME" --env-file "$ENV_FILE" build
fi

echo "🔄 [2/3] Quick-swapping container..."
if [ -n "$PROJECT_NAME" ]; then
    COMPOSE_PROJECT_NAME=$PROJECT_NAME docker-compose --env-file "$ENV_FILE" up -d --no-build "$SERVICE_NAME"
else
    # Detect if we should use specific compose file
    if [[ "$SERVICE_NAME" == *"sepolia"* ]]; then
        docker-compose -f docker-compose.testnet.yml --env-file "$ENV_FILE" up -d --no-build sepolia-indexer
    elif [[ "$SERVICE_NAME" == *"debug"* ]]; then
        docker-compose -f docker-compose.debug.yml --env-file "$ENV_FILE" up -d --no-build debug-indexer
    else
        docker-compose --env-file "$ENV_FILE" up -d --no-build "$SERVICE_NAME"
    fi
fi

echo "✅ [3/3] Deployment complete. Container is starting early-bird API..."
