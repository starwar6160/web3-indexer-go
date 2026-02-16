#!/bin/bash

# Generate demo transactions on Anvil via JSON-RPC
# This script sends raw transactions directly to Anvil's RPC endpoint

RPC_URL="${RPC_URL:-http://localhost:8545}"
PRIVATE_KEY="ac0974bec39a17e36ba4a6b4d238ff944bacb476c6b8d6c1f02e8a3e7c4d5e6f"
FROM_ADDRESS="0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
TO_ADDRESS="0x70997970C51812e339D9B73b0245ad59e5E05a77"

echo "🚀 Generating demo transactions on Anvil..."
echo "RPC URL: $RPC_URL"
echo "From: $FROM_ADDRESS"
echo "To: $TO_ADDRESS"

# Get current nonce
NONCE_RESPONSE=$(curl -s -X POST "$RPC_URL" \
  -H "Content-Type: application/json" \
  -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getTransactionCount\",\"params\":[\"$FROM_ADDRESS\",\"latest\"],\"id\":1}")

NONCE=$(echo "$NONCE_RESPONSE" | grep -o '"result":"0x[^"]*"' | cut -d'"' -f4)
NONCE_DEC=$((16#${NONCE:2}))

echo "Current nonce: $NONCE_DEC"

# Send 5 simple ETH transfer transactions
for i in {1..5}; do
  CURRENT_NONCE=$((NONCE_DEC + i - 1))
  NONCE_HEX=$(printf "0x%x" $CURRENT_NONCE)
  
  # Create a simple ETH transfer transaction
  TX_DATA=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "method": "eth_sendTransaction",
  "params": [{
    "from": "$FROM_ADDRESS",
    "to": "$TO_ADDRESS",
    "value": "0x$(printf '%x' $((1000000000000000 + i * 100000000000000)))",
    "gas": "0x5208",
    "gasPrice": "0x$(printf '%x' 1000000000)",
    "nonce": "$NONCE_HEX"
  }],
  "id": $((i + 1))
}
EOF
)
  
  TX_HASH=$(curl -s -X POST "$RPC_URL" \
    -H "Content-Type: application/json" \
    -d "$TX_DATA" | grep -o '"result":"0x[^"]*"' | cut -d'"' -f4)
  
  if [ -n "$TX_HASH" ]; then
    echo "✅ TX $i sent: $TX_HASH"
  else
    echo "❌ TX $i failed"
  fi
  
  sleep 1
done

echo ""
echo "✨ Demo transactions complete!"
echo "📊 Check Dashboard at http://localhost:8080 to see the transactions"
