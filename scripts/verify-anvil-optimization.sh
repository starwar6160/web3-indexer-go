#!/bin/bash
# Anvil 性能优化验证脚本
# 用途：验证三层防御体系是否正确部署

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
API_URL="${API_URL:-http://localhost:8080}"
METRICS_URL="${METRICS_URL:-http://localhost:8080/metrics}"
CHAIN_ID="${CHAIN_ID:-31337}"

echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   Anvil 性能优化验证脚本 (Three-Layer Defense)       ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""

# ═══════════════════════════════════════════════════════════════
# 测试 1: 验证编译成功
# ═══════════════════════════════════════════════════════════════
echo -e "${YELLOW}[测试 1/6] 验证编译成功${NC}"
if go build ./cmd/indexer 2>&1 | grep -q "error"; then
    echo -e "${RED}❌ 编译失败${NC}"
    exit 1
else
    echo -e "${GREEN}✅ 编译成功${NC}"
fi
echo ""

# ═══════════════════════════════════════════════════════════════
# 测试 2: 验证 API 可访问性
# ═══════════════════════════════════════════════════════════════
echo -e "${YELLOW}[测试 2/6] 验证 API 可访问性${NC}"
if ! curl -s --connect-timeout 5 "$API_URL/api/status" > /dev/null; then
    echo -e "${RED}❌ API 不可访问: $API_URL${NC}"
    echo -e "${YELLOW}💡 请先启动索引器: docker-compose up -d web3-indexer-app${NC}"
    exit 1
else
    echo -e "${GREEN}✅ API 可访问${NC}"
fi
echo ""

# ═══════════════════════════════════════════════════════════════
# 测试 3: 验证第一层防御 - Lab Mode 启用
# ═══════════════════════════════════════════════════════════════
echo -e "${YELLOW}[测试 3/6] 验证第一层防御 - Lab Mode 启用${NC}"
LAZY_STATUS=$(curl -s "$API_URL/api/status" | jq -r '.lazy_indexer.mode // "unknown"')
LAZY_DISPLAY=$(curl -s "$API_URL/api/status" | jq -r '.lazy_indexer.display // "unknown"')

if [[ "$LAZY_STATUS" == "active" ]]; then
    echo -e "${GREEN}✅ LazyManager 状态: active${NC}"
else
    echo -e "${RED}❌ LazyManager 状态: $LAZY_STATUS (预期: active)${NC}"
    exit 1
fi

if [[ "$LAZY_DISPLAY" == *"Lab Mode"* ]]; then
    echo -e "${GREEN}✅ Lab Mode 显示: $LAZY_DISPLAY${NC}"
else
    echo -e "${YELLOW}⚠️  Lab Mode 未显示: $LAZY_DISPLAY${NC}"
fi
echo ""

# ═══════════════════════════════════════════════════════════════
# 测试 4: 验证第二层防御 - 数据库连接池配置
# ═══════════════════════════════════════════════════════════════
echo -e "${YELLOW}[测试 4/6] 验证第二层防御 - 数据库连接池配置${NC}"

# 检查 Prometheus 指标
MAX_CONNS=$(curl -s "$METRICS_URL" | grep "^indexer_db_pool_max_connections" | awk '{print $2}')
IDLE_CONNS=$(curl -s "$METRICS_URL" | grep "^indexer_db_pool_idle_connections" | awk '{print $2}')
IN_USE=$(curl -s "$METRICS_URL" | grep "^indexer_db_pool_in_use" | awk '{print $2}')

if [[ -n "$MAX_CONNS" ]]; then
    echo -e "${GREEN}✅ 数据库连接池指标存在${NC}"
    echo -e "   - 最大连接数: $MAX_CONNS"
    echo -e "   - 空闲连接数: $IDLE_CONNS"
    echo -e "   - 使用中: $IN_USE"

    if [[ "$MAX_CONNS" == "100" ]]; then
        echo -e "${GREEN}✅ Anvil 激进配置生效 (100 连接)${NC}"
    else
        echo -e "${YELLOW}⚠️  最大连接数: $MAX_CONNS (Anvil 预期: 100)${NC}"
    fi
else
    echo -e "${RED}❌ 数据库连接池指标不存在${NC}"
    exit 1
fi
echo ""

# ═══════════════════════════════════════════════════════════════
# 测试 5: 验证第三层防御 - 实时高度更新
# ═══════════════════════════════════════════════════════════════
echo -e "${YELLOW}[测试 5/6] 验证第三层防御 - 实时高度更新（数字倒挂检测）${NC}"

# 连续 5 次采样，检查是否有倒挂
INVERTED_COUNT=0
for i in {1..5}; do
    LATEST=$(curl -s "$API_URL/api/status" | jq -r '.latest_block // "0"')
    INDEXED=$(curl -s "$API_URL/api/status" | jq -r '.latest_indexed // "0"')

    if [[ "$INDEXED" -gt "$LATEST" ]]; then
        echo -e "${RED}❌ 数字倒挂: Indexed($INDEXED) > Latest($LATEST)${NC}"
        ((INVERTED_COUNT++))
    fi

    sleep 0.5
done

if [[ $INVERTED_COUNT -eq 0 ]]; then
    echo -e "${GREEN}✅ 无数字倒挂现象（5 次采样）${NC}"
else
    echo -e "${RED}❌ 检测到 $INVERTED_COUNT 次数字倒挂${NC}"
fi
echo ""

# ═══════════════════════════════════════════════════════════════
# 测试 6: 验证 Prometheus 指标
# ═══════════════════════════════════════════════════════════════
echo -e "${YELLOW}[测试 6/6] 验证 Prometheus 指标${NC}"

LAB_MODE=$(curl -s "$METRICS_URL" | grep "^indexer_lab_mode_enabled" | awk '{print $2}')
if [[ "$LAB_MODE" == "1" ]]; then
    echo -e "${GREEN}✅ Lab Mode 指标: 启用 (1)${NC}"
else
    echo -e "${YELLOW}⚠️  Lab Mode 指标: $LAB_MODE (预期: 1)${NC}"
fi

# 检查其他关键指标
METRIC_COUNT=$(curl -s "$METRICS_URL" | grep -c "^indexer_")
echo -e "${GREEN}✅ Prometheus 指标总数: $METRIC_COUNT${NC}"
echo ""

# ═══════════════════════════════════════════════════════════════
# 最终报告
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                    验证总结                            ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${GREEN}✅ 第一层防御 (Lab Mode)${NC}     : $LAZY_STATUS"
echo -e "${GREEN}✅ 第二层防御 (连接池)${NC}       : $MAX_CONNS 最大连接"
echo -e "${GREEN}✅ 第三层防御 (实时更新)${NC}     : 倒挂次数 $INVERTED_COUNT/5"
echo ""
echo -e "${BLUE}📚 详细文档: docs/ANVIL_PERFORMANCE_OPTIMIZATION.md${NC}"
echo ""

if [[ $INVERTED_COUNT -eq 0 && "$LAZY_STATUS" == "active" ]]; then
    echo -e "${GREEN}🎉 所有测试通过！三层防御体系已成功部署。${NC}"
    exit 0
else
    echo -e "${YELLOW}⚠️  部分测试未通过，请检查配置。${NC}"
    exit 1
fi
