#!/bin/bash
# 快速测试 Anvil 修复效果

echo "=== 🧪 Anvil 修复快速验证 ==="
echo ""

# 1. 清理旧进程
echo "1️⃣ 清理旧进程..."
lsof -ti:8092 | xargs kill -9 2>/dev/null || true
sleep 1
echo "✅ 端口已清理"
echo ""

# 2. 检测 Anvil 高度
echo "2️⃣ 检测 Anvil 当前高度..."
ANVIL_HEIGHT=$(scripts/get-anvil-height.sh)
echo "📊 Anvil 高度: $ANVIL_HEIGHT"
echo ""

# 3. 启动 indexer（后台 10 秒）
echo "3️⃣ 启动 indexer（10 秒测试）..."
timeout 10s make test-a2 2>&1 | grep -E "(Anvil 当前高度|START_BLOCK|Rate limiter|Engine Components Ignited)" &
TEST_PID=$!

sleep 12
kill $TEST_PID 2>/dev/null || true

echo ""
echo "=== ✅ 验证完成 ==="
echo ""
echo "预期看到的日志："
echo "  📊 Anvil 当前高度: <数字>"
echo '  "🎯 Using START_BLOCK from config","block":"<检测到的高度>"'
echo '  "✅ Rate limiter configured","rps":500,"mode":"local"'
echo '  "⛓️ Engine Components Ignited","start_block":"<检测到的高度>"'
echo ""
echo "如果看到上述日志，说明修复成功！🎉"
