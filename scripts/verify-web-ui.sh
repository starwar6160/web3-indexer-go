#!/bin/bash
# 验证 Web UI 是否显示 Synthetic Transfers

echo "=== 🌐 Web UI 验证脚本 ==="
echo ""

# 1. 检查 indexer 是否运行
echo "1️⃣ 检查 Indexer 状态..."
if curl -s http://localhost:8092/api/status > /dev/null; then
    echo "✅ Indexer 正在运行 (端口 8092)"
else
    echo "❌ Indexer 未运行"
    exit 1
fi

echo ""

# 2. 检查数据库中的 transfer 数量
echo "2️⃣ 检查数据库..."
TRANSFER_COUNT=$(PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -t -c "SELECT COUNT(*) FROM transfers;")
echo "📊 Transfers: $TRANSFER_COUNT"

if [ "$TRANSFER_COUNT" -eq 0 ]; then
    echo "❌ 数据库为空，需要注入模拟数据"
    echo "💡 运行: psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -f scripts/inject-mock-transfers.sql"
    exit 1
fi

echo ""

# 3. 检查 API
echo "3️⃣ 检查 API 响应..."
API_RESPONSE=$(curl -s http://localhost:8092/api/transfers?limit=5)
echo "📡 API 返回:"
echo "$API_RESPONSE" | jq '.transfers | length' | xargs echo "  Transfers 数量:"

echo ""

# 4. 生成 URL
echo "4️⃣ 访问 Web UI:"
echo "   🌐 打开浏览器: http://localhost:8092"
echo "   或按 Ctrl+Click 打开: "
echo ""

# 检测操作系统
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo "   xdg-open http://localhost:8092"
elif [[ "$OSTYPE" == "darwin"* ]]; then
    echo "   open http://localhost:8092"
fi

echo ""
echo "=== ✅ 验证完成 ==="
echo ""
echo "预期结果:"
echo "  📊 Dashboard 应显示: Total (Synced) = $TRANSFER_COUNT"
echo "  📋 Latest Transfers 表格应该显示 5-10 条记录"
echo "  🔄 Real-time TPS 图表应该开始更新"
echo ""
echo "如果网页仍然显示空，检查:"
echo "  1. 浏览器控制台是否有错误"
echo "  2. WebSocket 连接是否成功 (ws://localhost:8092/ws)"
echo "  3. 网络请求是否返回 200"
