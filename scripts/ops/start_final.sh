#!/bin/bash
export DATABASE_URL="postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable"
export RPC_URLS="http://localhost:8545"
export CHAIN_ID="31337"
export START_BLOCK="0"
export EMULATOR_ENABLED="true"
export EMULATOR_RPC_URL="http://localhost:8545"
# Anvil Account 0 PK
export EMULATOR_PRIVATE_KEY="ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
export EMULATOR_BLOCK_INTERVAL="1s"
export EMULATOR_TX_INTERVAL="2s"
export API_PORT="8080"
export LOG_LEVEL="info"
export LOG_FORMAT="json"
# Overclock settings
export FETCH_BATCH_SIZE=2000
export FETCH_CONCURRENCY=50

echo "Starting Indexer..."
./indexer
