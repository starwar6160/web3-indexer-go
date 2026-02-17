#!/bin/bash
# 检测 Anvil 当前高度并输出为 START_BLOCK 环境变量

ANVIL_URL="${ANVIL_URL:-http://127.0.0.1:8545}"

# 使用 curl + jq 获取当前高度
CURRENT_HEIGHT=$(curl -s -X POST "$ANVIL_URL" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  | jq -r '.result' \
  | sed 's/^0x//' \
  | awk '{print strtonum("0x" $1)}')

# 如果失败，返回 0
if [ -z "$CURRENT_HEIGHT" ] || [ "$CURRENT_HEIGHT" -lt 0 ]; then
    echo "0"
else
    echo "$CURRENT_HEIGHT"
fi
