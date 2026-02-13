#!/bin/bash

# ==============================================================================
# Web3 Indexer 配置验证脚本
# ==============================================================================

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== 验证配置安全性 ===${NC}"

# 检查必需的环境变量
MISSING_VARS=()

if [[ -z "$DATABASE_URL" ]]; then
    MISSING_VARS+=("DATABASE_URL")
fi

if [[ -z "$RPC_URLS" ]]; then
    MISSING_VARS+=("RPC_URLS")
fi

if [[ ${#MISSING_VARS[@]} -gt 0 ]]; then
    echo -e "${RED}❌ 缺少必需的环境变量: ${MISSING_VARS[*]}${NC}"
    exit 1
fi

# 检查是否在演示模式下使用了正确的配置
if [[ "$DEMO_MODE" == "true" ]]; then
    # 验证演示模式的安全配置
    if [[ "$DATABASE_URL" != *"W3b3_Idx_Secur3_2026_Sec"* ]]; then
        echo -e "${YELLOW}⚠️  演示模式下建议使用默认数据库密码${NC}"
    fi
    
    if [[ "$RPC_URLS" != *"anvil"* && "$RPC_URLS" != *"localhost"* && "$RPC_URLS" != *"127.0.0.1"* ]]; then
        echo -e "${RED}❌ 演示模式下不允许使用外部RPC节点${NC}"
        exit 1
    fi
fi

echo -e "${GREEN}✅ 配置验证通过${NC}"