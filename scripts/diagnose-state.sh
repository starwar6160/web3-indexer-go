#!/bin/bash
# 状态诊断脚本 - 排查"索引器领先于链"问题

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "🔍 Web3 Indexer 状态诊断"
echo "======================================="
echo ""

# 1. 检查所有索引器进程
echo "📊 1. 检查运行的索引器进程"
echo "-----------------------------------"
ps aux | grep "[i]ndexer" | grep -v wind | while read line; do
    pid=$(echo $line | awk '{print $2}')
    echo "PID: $pid"
done

echo ""

# 2. 检查端口占用
echo "📡 2. 检查端口占用"
echo "-----------------------------------"
for port in 8080 8081 8082 8092; do
    if lsof -ti:$port > /dev/null 2>&1; then
        pid=$(lsof -ti:$port)
        echo -e "${GREEN}✓ Port $port: 进程 $pid${NC}"

        # 尝试获取状态
        if curl -s http://localhost:$port/api/status > /dev/null 2>&1; then
            status=$(curl -s http://localhost:$port/api/status)
            latest=$(echo $status | jq -r '.latest_block // "N/A"')
            indexed=$(echo $status | jq -r '.latest_indexed // "N/A"')
            state=$(echo $status | jq -r '.state // "N/A"')

            echo "  Latest: $latest"
            echo "  Indexed: $indexed"
            echo "  State: $state"

            # 检查悖论
            if [ "$latest" != "N/A" ] && [ "$indexed" != "N/A" ]; then
                latest_int=$((latest))
                indexed_int=$((indexed))

                if [ $indexed_int -gt $latest_int ]; then
                    gap=$((indexed_int - latest_int))
                    echo -e "  ${RED}⚠️  PARADOX_DETECTED: 领先 $gap 块${NC}"
                else
                    lag=$((latest_int - indexed_int))
                    echo -e "  ${GREEN}✓ 状态正常：滞后 $lag 块${NC}"
                fi
            fi
        fi
    else
        echo -e "${YELLOW}✗ Port $port: 未监听${NC}"
    fi
    echo ""
done

# 3. 检查 Anvil 高度
echo "⛓️  3. 检查 Anvil 链高度"
echo "-----------------------------------"
if curl -s http://127.0.0.1:8545 > /dev/null 2>&1; then
    anvil_height=$(curl -s -X POST http://127.0.0.1:8545 \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        | jq -r '.result' | xargs printf "%d\n")
    echo -e "${GREEN}✓ Anvil 高度: $anvil_height${NC}"
else
    echo -e "${RED}✗ Anvil 未运行${NC}"
fi

echo ""

# 4. 检查数据库
echo "💾 4. 检查数据库状态"
echo "-----------------------------------"
if PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -c "SELECT 1" > /dev/null 2>&1; then
    max_block=$(PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -t -c "SELECT COALESCE(MAX(number), 0) FROM blocks;")
    echo -e "${GREEN}✓ 数据库最大块: $max_block${NC}"
else
    echo -e "${YELLOW}✗ 数据库连接失败${NC}"
fi

echo ""

# 5. 检查最近的日志
echo "📝 5. 检查最近的错误日志"
echo "-----------------------------------"
if docker logs web3-indexer-app 2>&1 | tail -50 | grep -q "REALITY_PARADOX"; then
    echo -e "${RED}⚠️  检测到现实悖论日志${NC}"
    docker logs web3-indexer-app 2>&1 | grep "REALITY_PARADOX" | tail -5
else
    echo -e "${GREEN}✓ 未检测到现实悖论${NC}"
fi

echo ""

# 6. 总结
echo "🎯 诊断建议"
echo "-----------------------------------"
echo "如果看到 'PARADOX_DETECTED'："
echo "1. 检查 Anvil 是否重启过（高度回落）"
echo "2. 重启索引器容器（会触发现实坍缩）"
echo "3. 或等待 30 秒让运行时审计自动修复"
echo ""
echo "如果端口不是预期的："
echo "- 8080: Demo (未使用)"
echo "- 8081: Sepolia 测试网"
echo "- 8082: Anvil Demo (Docker)"
echo "- 8092: Anvil Local (make test-a2)"
echo ""
echo "清除浏览器缓存后刷新页面"
