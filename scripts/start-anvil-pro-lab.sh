#!/bin/bash
# 🏭 Anvil Pro 实验室启动脚本
# 一键启动：Indexer + Pro Simulator + 验证

set -e

echo "=== 🏭 Anvil Pro 实验室 ==="
echo ""

# 1. 检查 Anvil 是否运行
echo "1️⃣ 检查 Anvil 状态..."
if ! curl -s http://127.0.0.1:8545 -X POST \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' > /dev/null; then
    echo "❌ Anvil 未运行！请先启动 Anvil："
    echo "   docker start web3-demo2-anvil"
    echo "   或: anvil --host 0.0.0.0"
    exit 1
fi

ANVIL_HEIGHT=$(scripts/get-anvil-height.sh)
echo "✅ Anvil 运行中 (高度: $ANVIL_HEIGHT)"
echo ""

# 2. 停止旧的 indexer
echo "2️⃣ 清理旧进程..."
lsof -ti:8092 | xargs kill -9 2>/dev/null || true
sleep 1
echo "✅ 端口 8092 已清理"
echo ""

# 3. 重置数据库（可选）
read -p "是否重置数据库？(y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    make anvil-reset
    echo ""
fi

# 4. 启动 Indexer（后台）
echo "3️⃣ 启动 Indexer (后台)..."
export PORT=8092
export RPC_URLS="http://127.0.0.1:8545"
export CHAIN_ID=31337
export START_BLOCK=0
export DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@127.0.0.1:15432/web3_demo?sslmode=disable"
export APP_TITLE="🏭 ANVIL-PRO-LAB"
export DEMO_MODE=false
export RPC_RATE_LIMIT=500

go run cmd/indexer/*.go > /tmp/anvil-pro-lab.log 2>&1 &
INDEXER_PID=$!

echo "✅ Indexer 已启动 (PID: $INDEXER_PID)"
echo "   日志: tail -f /tmp/anvil-pro-lab.log"
echo ""

# 5. 等待 Indexer 就绪
echo "4️⃣ 等待 Indexer 就绪..."
for i in {1..10}; do
    if curl -s http://localhost:8092/api/status > /dev/null; then
        echo "✅ Indexer 就绪！"
        break
    fi
    echo "   等待中... ($i/10)"
    sleep 1
done
echo ""

# 6. 显示访问信息
echo "=== ✅ Anvil Pro 实验室已启动 ==="
echo ""
echo "🌐 Web UI:"
echo "   http://localhost:8092"
echo ""
echo "📊 实时日志:"
echo "   tail -f /tmp/anvil-pro-lab.log"
echo ""
echo "🎯 Pro Simulator:"
echo "   ✅ 自动运行 (每秒 2 笔交易)"
echo "   ✅ 随机金额 (非整数)"
echo "   ✅ 多代币支持 (USDC/USDT/WBTC/WETH/DAI)"
echo ""
echo "📈 预期效果:"
echo "   □ Latest Transfers 表格开始滚动"
echo "   □ Real-time TPS 图表更新"
echo "   □ 金额显示为 123.456 USDC (非 1/2/3 ETH)"
echo "   □ Token Symbol 显示 (USDC 而非 0x...)"
echo ""
echo "🛑 停止实验室:"
echo "   kill $INDEXER_PID"
echo "   或按 Ctrl+C"
echo ""

# 7. 自动打开浏览器（可选）
if command -v xdg-open > /dev/null; then
    xdg-open http://localhost:8092 > /dev/null 2>&1 &
elif command -v open > /dev/null; then
    open http://localhost:8092 > /dev/null 2>&1 &
fi

# 8. 持续监控日志
echo "=== 📊 实时监控 ==="
echo "按 Ctrl+C 停止监控（Indexer 继续运行）"
echo ""

tail -f /tmp/anvil-pro-lab.log &
TAIL_PID=$!

# 等待用户中断
trap "echo ''; echo '🛑 停止监控...'; kill $TAIL_PID 2>/dev/null; echo '✅ Indexer 仍在运行 (PID: $INDEXER_PID)'; echo '停止 Indexer: kill $INDEXER_PID'; exit 0" INT TERM

wait
