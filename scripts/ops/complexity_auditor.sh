#!/bin/bash
# ==============================================================================
# 🧩 Complexity Auditor - 工业级代码复杂度审计工具
# ==============================================================================
# 功能：扫描项目中逻辑最复杂的函数，生成重构建议单。
# 依赖：gocyclo (go install github.com/fzipp/gocyclo/cmd/gocyclo@latest)
# ==============================================================================

set -euo pipefail

# 颜色输出
BLUE='\033[34m'
GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
NC='\033[0m'

# 阈值配置
COMPLEXITY_THRESHOLD=15
TOP_N=10

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}   🧩 启动 Complexity Auditor (工业级代码审计)${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"

# 检查依赖
if ! command -v gocyclo &> /dev/null; then
    echo -e "${YELLOW}📦 正在安装 gocyclo...${NC}"
    go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
fi

echo -e "${YELLOW}🔍 正在扫描全量代码库 (Top $TOP_N 复杂度函数)...${NC}"
echo ""

# 运行扫描并提取 Top N
AUDIT_RESULTS=$(gocyclo -top $TOP_N . | grep -v "_test.go")

echo -e "${BLUE}📊 审计结果详情：${NC}"
echo "------------------------------------------------------------"
printf "%-10s %-20s %-30s
" "Complexity" "Function" "Location"
echo "------------------------------------------------------------"

while read -r line; do
    COMPLEXITY=$(echo "$line" | awk '{print $1}')
    FUNC=$(echo "$line" | awk '{print $2}')
    LOCATION=$(echo "$line" | awk '{print $3}')
    
    # 根据复杂度着色
    if [ "$COMPLEXITY" -gt 25 ]; then
        COLOR=$RED
        STATUS="[🚨 重构建议：极高]"
    elif [ "$COMPLEXITY" -gt 15 ]; then
        COLOR=$YELLOW
        STATUS="[⚠️  重构建议：中等]"
    else
        COLOR=$GREEN
        STATUS="[✅ 状态：健康]"
    fi
    
    printf "${COLOR}%-10s${NC} %-20s %-30s %s
" "$COMPLEXITY" "$FUNC" "$LOCATION" "$STATUS"
done <<< "$AUDIT_RESULTS"

echo "------------------------------------------------------------"
echo ""

# 生成重构建议摘要
HIGH_COMPLEXITY_COUNT=$(echo "$AUDIT_RESULTS" | awk '$1 > 15' | wc -l)

if [ "$HIGH_COMPLEXITY_COUNT" -gt 0 ]; then
    echo -e "${YELLOW}💡 资深架构师重构建议：${NC}"
    echo "   1. 发现 $HIGH_COMPLEXITY_COUNT 个函数复杂度超过阈值 ($COMPLEXITY_THRESHOLD)。"
    echo "   2. 建议将超过 20 层的 Switch 或 If-Else 链提取为 Strategy 模式或 Map 查找。"
    echo "   3. 检查解析以太坊交易的核心逻辑，尝试通过子函数拆分降低主流程压力。"
else
    echo -e "${GREEN}✅ 代码健康度良好，未发现明显的技术债（复杂度维度）。${NC}"
fi

echo ""
echo -e "${GREEN}✅ 复杂度审计完成。${NC}"
