#!/bin/bash

# ==============================================================================
# Web3 Indexer 配置安全检查脚本
# ==============================================================================

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== 配置安全检查 ===${NC}"

# 检查是否存在硬编码的敏感信息
echo -e "${YELLOW}检查硬编码的敏感信息...${NC}"

FOUND_ISSUES=0

# 检查硬编码的私钥
if grep -r "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80" . --exclude-dir=.git --exclude="CONFIGURATION.md" --exclude="*.json" --exclude-dir=config --exclude-dir=setup 2>/dev/null | grep -v "Binary file" | head -10; then
    echo -e "${YELLOW}⚠️  发现硬编码的演示私钥（仅适用于演示模式）${NC}"
else
    echo -e "${GREEN}✅ 未发现演示私钥硬编码（除了配置文件）${NC}"
fi

# 检查硬编码的数据库密码
if grep -r "W3b3_Idx_Secur3_2026_Sec" . --exclude-dir=.git --exclude="CONFIGURATION.md" --exclude="*.json" --exclude-dir=config --exclude-dir=setup 2>/dev/null | grep -v "Binary file" | head -10; then
    echo -e "${YELLOW}⚠️  发现硬编码的演示数据库密码（仅适用于演示模式）${NC}"
else
    echo -e "${GREEN}✅ 未发现数据库密码硬编码（除了配置文件）${NC}"
fi

echo -e "\n${GREEN}✅ 配置安全检查完成${NC}"
echo -e "${BLUE}💡 提示：演示模式的硬编码信息仅适用于本地开发和演示${NC}"