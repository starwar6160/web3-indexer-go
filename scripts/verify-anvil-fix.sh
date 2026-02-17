#!/bin/bash
# 验证 Anvil 环境修复是否成功

echo "=== 测试 1: Anvil 自动高度检测 ==="
ANVIL_HEIGHT=$(scripts/get-anvil-height.sh)
echo "📊 Anvil 当前高度: $ANVIL_HEIGHT"

echo ""
echo "=== 测试 2: 检查配置文件 START_BLOCK ==="
grep "START_BLOCK=" configs/env/.env.demo2

echo ""
echo "=== 测试 3: 编译验证 ==="
go build -o /tmp/web3-indexer-test ./cmd/indexer 2>&1
if [ $? -eq 0 ]; then
    echo "✅ 编译成功"
else
    echo "❌ 编译失败"
    exit 1
fi

echo ""
echo "=== 预期行为 ==="
echo "1. ✅ getDefaultStartBlock(31337) 应返回 0"
echo "2. ✅ START_BLOCK=0 应被正确识别（不会被 > 0 跳过）"
echo "3. ✅ 智能 RPS: 本地模式应为 500"
echo "4. ✅ make test-a2 应从 $ANVIL_HEIGHT 开始（而非 10262444）"

echo ""
echo "=== 手动验证命令 ==="
echo "make test-a2"
echo "# 观察日志中的："
echo "#   🎯 Using START_BLOCK from config block=0"
echo "#   🧠 Smart Rate Limiter initialized: 500.00 RPS"
echo "#   ⛓️ Engine Components Ignited start_block=0"
