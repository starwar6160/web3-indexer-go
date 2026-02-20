#!/usr/bin/env bash
# run_race_test.sh
# Usage: MODE=<mode> ./scripts/run_race_test.sh
# Runs the Go race detector across the codebase.
set -e

if [ -z "$MODE" ]; then
  echo "MODE not set, defaulting to 'testnet'"
  MODE="testnet"
fi

echo "🏁 Running Go race detector for mode: $MODE"
# Export any mode-specific env vars if needed (placeholder)
# Example: export TESTNET=true for testnet mode
if [ "$MODE" = "testnet" ]; then
  export TESTNET=true
else
  export ANVIL=true
fi

go test -race ./... 
