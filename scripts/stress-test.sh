#!/bin/bash
# 🚀 高频交易压测脚本
# 测试数据库索引和前端渲染的极限性能

set -e

TRANSFER_COUNT=1000  # 注入 1000 笔交易
BLOCK_START=60500    # 从块 60500 开始
TPS_TARGET=100       # 目标 TPS（每秒交易数）

echo "=== 🚀 高频交易压测 ==="
echo "目标: 注入 $TRANSFER_COUNT 笔交易"
echo "区块范围: $BLOCK_START - $((BLOCK_START + TRANSFER_COUNT / 10))"
echo ""

# 检查数据库连接
echo "1️⃣ 检查数据库连接..."
if ! PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -c "SELECT 1;" > /dev/null 2>&1; then
    echo "❌ 数据库连接失败"
    exit 1
fi
echo "✅ 数据库连接正常"
echo ""

# 清空旧数据（可选）
read -p "是否清空旧测试数据？(y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🧹 清空旧测试数据..."
    PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
        -c "DELETE FROM transfers WHERE block_number >= $BLOCK_START;"
    echo "✅ 旧数据已清空"
    echo ""
fi

# 生成压测数据
echo "2️⃣ 生成压测数据（$TRANSFER_COUNT 笔）..."
echo "这可能需要几秒钟..."

START_TIME=$(date +%s)

# 使用 SQL 生成批量数据
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo << EOF 2>&1 | grep -v "INSERT"
WITH
-- 生成区块序列
blocks AS (
    SELECT generate_series($BLOCK_START, $BLOCK_START + 100) as block_number
),
-- 生成每块的多笔交易（模拟高频）
transfers AS (
    SELECT
        b.block_number,
        '0x' || encode(sha256(b.block_number::text || n::text), 'hex') as tx_hash,
        n as log_index,
        (ARRAY['0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266',
                '0x70997970c51812dc3a010c7d01b50e0d17dc79c8',
                '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc',
                '0x90f79bf6eb2c4f870365e785982e1f101e93b906',
                '0x15d34aaf54267db7d7c367839aaf71a00a2c6a65',
                '0x742d35cc6634c0532925a3b844bc9e7595f0beb0'])[n % 6] as from_address,
        (ARRAY['0xE592427A0AEce92De3Edee1F18E0157C05861564',
                '0xbEbc44782C7dB0a1A60Cb6fe97d0b483032FF1C7',
                '0xBA12222222228d8Ba445958a75a0704d566BF2C8'])[n % 3] as to_address,
        (1000000 + (n * 1000) * 1000000)::text::numeric as amount,
        (ARRAY['0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48',
                '0xdAC17F958D2ee523a2206206994597C13D831ec7',
                '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2',
                '0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599'])[n % 4] as token_address
    FROM blocks b
    CROSS JOIN generate_series(0, 9) n  -- 每块 10 笔交易
    LIMIT $TRANSFER_COUNT
)
INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
SELECT * FROM transfers
ON CONFLICT (block_number, log_index) DO NOTHING;
EOF

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo ""
echo "✅ 数据注入完成"
echo "⏱️  耗时: ${DURATION} 秒"
echo "📊 实际 TPS: $((TRANSFER_COUNT / DURATION))"
echo ""

# 验证数据
echo "3️⃣ 验证数据..."
TOTAL_TRANSFERS=$(PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -t -c "SELECT COUNT(*) FROM transfers WHERE block_number >= $BLOCK_START;")
echo "📊 Transfers: $TOTAL_TRANSFERS"
echo ""

# 分析数据分布
echo "4️⃣ 数据分布分析："
echo "按代币分布："
PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo << EOF
SELECT
    CASE token_address
        WHEN '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48' THEN 'USDC'
        WHEN '0xdAC17F958D2ee523a2206206994597C13D831ec7' THEN 'USDT'
        WHEN '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2' THEN 'WETH'
        WHEN '0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599' THEN 'WBTC'
        ELSE 'Unknown'
    END as token,
    COUNT(*) as count,
    ROUND(COUNT(*)::numeric / $TOTAL_TRANSFERS * 100, 2) as percentage
FROM transfers
WHERE block_number >= $BLOCK_START
GROUP BY token_address
ORDER BY count DESC;
EOF

echo ""
echo "5️⃣ 测试 API 查询性能..."
API_START=$(date +%s%N)
curl -s "http://localhost:8092/api/transfers?limit=100&offset=0" > /dev/null
API_END=$(date +%s%N)
API_MS=$(( (API_END - API_START) / 1000000 ))
echo "📡 API 响应时间: ${API_MS}ms"

if [ $API_MS -lt 100 ]; then
    echo "✅ API 性能优秀（< 100ms）"
elif [ $API_MS -lt 500 ]; then
    echo "⚠️  API 性能一般（100-500ms）"
else
    echo "❌ API 性能较差（> 500ms）"
fi

echo ""
echo "=== ✅ 压测完成 ==="
echo ""
echo "💡 访问 Web UI 查看效果："
echo "   🌐 http://localhost:8092"
echo ""
echo "📊 检查项："
echo "   □ Latest Transfers 表格是否流畅滚动"
echo "   □ Real-time TPS 图表是否更新"
echo "   □ 浏览器 CPU 占用是否合理"
echo "   □ 前端是否有卡顿或崩溃"
echo ""
echo "🔧 如果前端卡顿："
echo "   1. 检查浏览器控制台错误"
echo "   2. 降低 TRANSFER_COUNT 重试"
echo "   3. 添加虚拟滚动（Virtual Scrolling）"
