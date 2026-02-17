#!/bin/bash
# 快速测试 Anvil 修复

set -e

echo "=== 启动 Anvil Indexer（后台）==="
export PORT=8092
export RPC_URLS="http://127.0.0.1:8545"
export CHAIN_ID=31337
export START_BLOCK=0
export DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@127.0.0.1:15432/web3_demo?sslmode=disable"
export APP_TITLE="🧪 ANVIL-TEST"
export DEMO_MODE=false
export RPC_RATE_LIMIT=500

timeout 30s go run cmd/indexer/*.go 2>&1 | tee /tmp/indexer-test.log &

PID=$!
echo "Indexer PID: $PID"

# 等待 15 秒观察日志
echo "等待 15 秒观察启动日志..."
sleep 15

echo ""
echo "=== 关键日志分析 ==="
echo "检查 START_BLOCK 识别："
grep -i "Using START_BLOCK from config" /tmp/indexer-test.log || echo "❌ 未找到 START_BLOCK 日志"

echo ""
echo "检查智能 RPS 初始化："
grep -i "Smart Rate Limiter initialized" /tmp/indexer-test.log || echo "❌ 未找到 RPS 日志"

echo ""
echo "检查 Engine 启动："
grep -i "Engine Components Ignited" /tmp/indexer-test.log || echo "❌ 未找到 Engine 启动日志"

echo ""
echo "=== 停止测试进程 ==="
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true

echo "✅ 测试完成，完整日志: /tmp/indexer-test.log"
