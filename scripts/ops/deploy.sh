#!/bin/bash
# scripts/ops/deploy.sh - Industrial-grade deployment & cache alignment

set -e

# Configuration (Overrides via env vars recommended)
IMAGE_NAME="web3-indexer-go:stable"
PROJECT_NAME="web3-indexer"
INFRA_COMPOSE="configs/docker/docker-compose.infra.yml"
TESTNET_COMPOSE="configs/docker/docker-compose.testnet.yml"

echo "🚀 [STAGE 1] Performing engineering build (No-Cache)..."
docker build --no-cache -t $IMAGE_NAME .

echo "♻️ [STAGE 2] Force recreating instances to align logic..."
# Handle Sepolia
docker stop web3-testnet-app || true
docker rm web3-testnet-app || true
set -a; . configs/env/.env.testnet; set +a;
docker compose -p $PROJECT_NAME -f $TESTNET_COMPOSE up -d --no-build

# Handle Anvil Demo
docker stop web3-indexer-app || true
docker rm web3-indexer-app || true
set -a; . configs/env/.env.demo2; set +a;
COMPOSE_PROJECT_NAME=web3-demo2 docker compose -p $PROJECT_NAME -f configs/docker/docker-compose.yml up -d --no-build

echo "🧹 [STAGE 3] Purging Cloudflare Cache (if configured)..."
if [ -n "$CF_ZONE_ID" ] && [ -n "$CF_API_TOKEN" ]; then
    curl -s -X POST "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/purge_cache" \
         -H "Authorization: Bearer $CF_API_TOKEN" \
         -H "Content-Type: application/json" \
         --data '{"purge_everything":true}' | jq .
    echo "✅ Cloudflare edge purged."
else
    echo "⚠️ Skipping CF purge (CF_ZONE_ID/CF_API_TOKEN not set)."
fi

echo "✅ [SUCCESS] Deployment complete. Yokohama Lab results are now global."