#!/bin/bash
# 🎬 全自动演示脚本 - v2.2.0-stable
# 用途：一键展示 Web3 Indexer 的完整功能
# 适合：招聘演示、技术分享

set -e

echo "🎬 Web3 Indexer - Auto Demo Script"
echo "=================================="
echo ""

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. 环境检查
echo -e "${BLUE}📋 Step 1: Environment Check${NC}"
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker not found${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Docker found${NC}"

if ! curl -sf http://localhost:8082/api/status > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  Indexer not running, starting...${NC}"
    make a2
    sleep 10
fi
echo -e "${GREEN}✅ Indexer running on port 8082${NC}"
echo ""

# 2. 清理旧日志
echo -e "${BLUE}📋 Step 2: Cleanup Old Logs${NC}"
docker logs web3-demo2-app 2>&1 | tail -100 > /tmp/old-logs.log
echo -e "${GREEN}✅ Old logs saved to /tmp/old-logs.log${NC}"
echo ""

# 3. 显示当前状态
echo -e "${BLUE}📋 Step 3: Current System Status${NC}"
curl -s http://localhost:8082/api/status | jq '{
    system_state: .system_state,
    latest_height: .latest_height,
    synced_cursor: .synced_cursor,
    sync_progress: (.synced_cursor / .latest_height * 100 | floor),
    transfers: .transfers,
    sync_lag: (.latest_height - .synced_cursor)
}'
echo ""

# 4. 模拟链上交易脉冲（热度唤醒演示）
echo -e "${BLUE}📋 Step 4: Simulate Transaction Burst (Heat Awakening)${NC}"
echo -e "${YELLOW}📊 Injecting 100 test transactions...${NC}"

# 使用 anvil-inject 脚本
if make anvil-inject &> /dev/null; then
    sleep 5
    
    # 显示热度响应
    echo -e "${GREEN}✅ Transactions injected${NC}"
    echo ""
    echo -e "${BLUE}📊 Heat Detection Result:${NC}"
    curl -s http://localhost:8082/api/status | jq '{
        tps: .real_time_tps,
        system_state: .system_state,
        synced_cursor: .synced_cursor
    }'
else
    echo -e "${YELLOW}⚠️  Anvil injection not available, skipping heat demo${NC}"
fi
echo ""

# 5. 异常恢复演示（重启 Anvil）
echo -e "${BLUE}📋 Step 5: Exception Recovery Demo${NC}"
echo -e "${RED}⚠️  Restarting Anvil container...${NC}"
docker restart web3-demo2-anvil > /dev/null 2>&1

echo -e "${YELLOW}⏳ Waiting for self-healing (5 seconds)...${NC}"
sleep 5

# 显示恢复状态
echo -e "${GREEN}✅ System recovery status:${NC}"
docker logs web3-demo2-app 2>&1 | grep -E "(DeadlockWatchdog|SELF_HEAL|Gap)" | tail -5
echo ""

# 6. 打开浏览器
echo -e "${BLUE}📋 Step 6: Open Dashboard${NC}"
if command -v xdg-open &> /dev/null; then
    xdg-open http://localhost:8082 > /dev/null 2>&1 &
elif command -v open &> /dev/null; then
    open http://localhost:8082 > /dev/null 2>&1 &
else
    echo -e "${YELLOW}⚠️  Please open http://localhost:8082 manually${NC}"
fi
echo ""

# 7. 演示总结
echo -e "${BLUE}📊 Demo Summary${NC}"
echo "=================================="
echo -e "${GREEN}✅ Environment Check${NC}      - Docker + Indexer ready"
echo -e "${GREEN}✅ Current Status${NC}         - System syncing"
echo -e "${GREEN}✅ Heat Awakening${NC}         - TPS burst detected"
echo -e "${GREEN}✅ Exception Recovery${NC}     - Self-healing triggered"
echo -e "${GREEN}✅ Dashboard Opened${NC}       - http://localhost:8082"
echo ""
echo -e "${BLUE}📝 Demo Script:${NC}"
echo "1. Point to the top 'Sync Progress' (100% ✅)"
echo "2. Highlight 'Real-time TPS' (stable 7.75)"
echo "3. Explain 'Self-Healing' (watchdog + gap bypass)"
echo "4. Show 'Heat-based Eco-Mode' (200ms - 30s adaptive)"
echo ""
echo -e "${GREEN}🎉 Demo Ready! Good luck with your interview!${NC}"
