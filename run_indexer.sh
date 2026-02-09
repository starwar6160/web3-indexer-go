#!/bin/bash

# 🚀 Web3 Indexer - 持续运行模式启动脚本

set -e

cd "$(dirname "$0")"

echo "🚀 启动 Web3 Indexer（持续运行模式）..."
echo "📍 配置："
echo "   - CONTINUOUS_MODE: true（永不休眠）"
echo "   - RPC: http://localhost:8545"
echo "   - DB: localhost:15432"
echo "   - 监听合约: 0x5FC8d32690cc91D4c39d9d3abcBD16989F875707"
echo ""

CONTINUOUS_MODE=true \
RPC_URLS=http://localhost:8545 \
DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable \
CHAIN_ID=31337 \
START_BLOCK=0 \
WATCH_ADDRESSES=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707 \
API_PORT=8080 \
LOG_LEVEL=info \
LOG_FORMAT=json \
go run cmd/indexer/main.go
