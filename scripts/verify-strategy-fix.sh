#!/bin/bash
# 策略修复验证脚本
# 用于验证 test-a2 和 a2 的策略选择是否一致

set -e

echo "🔍 策略修复验证脚本"
echo "===================="
echo ""

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查配置文件
echo "📋 1. 检查配置文件 (.env.demo2)"
echo "-----------------------------------"
if grep -q "CHAIN_ID=31337" configs/env/.env.demo2; then
    echo -e "${GREEN}✅ CHAIN_ID=31337 (Anvil)${NC}"
else
    echo -e "${RED}❌ CHAIN_ID 配置错误${NC}"
    exit 1
fi

if grep -q "APP_MODE=EPHEMERAL_ANVIL" configs/env/.env.demo2; then
    echo -e "${GREEN}✅ APP_MODE=EPHEMERAL_ANVIL${NC}"
else
    echo -e "${RED}❌ APP_MODE 配置错误${NC}"
    exit 1
fi
echo ""

# 检查 Docker Compose 配置
echo "📋 2. 检查 Docker Compose 配置"
echo "-----------------------------------"
if grep -q "APP_MODE=\${APP_MODE:-EPHEMERAL_ANVIL}" configs/docker/docker-compose.yml; then
    echo -e "${GREEN}✅ docker-compose.yml 包含 APP_MODE 默认值${NC}"
else
    echo -e "${RED}❌ docker-compose.yml 缺少 APP_MODE 配置${NC}"
    exit 1
fi

if grep -q "CHAIN_ID=\${CHAIN_ID:-31337}" configs/docker/docker-compose.yml; then
    echo -e "${GREEN}✅ docker-compose.yml 包含 CHAIN_ID 默认值${NC}"
else
    echo -e "${RED}❌ docker-compose.yml 缺少 CHAIN_ID 配置${NC}"
    exit 1
fi
echo ""

# 检查 Go 代码
echo "📋 3. 检查 Go 代码修复"
echo "-----------------------------------"
if grep -q "APP_MODE override" internal/engine/factory.go; then
    echo -e "${GREEN}✅ factory.go 包含 APP_MODE 优先级检查${NC}"
else
    echo -e "${RED}❌ factory.go 缺少 APP_MODE 优先级检查${NC}"
    exit 1
fi

if grep -q "STRATEGY] Factory Initialization" cmd/indexer/main.go; then
    echo -e "${GREEN}✅ main.go 包含策略工厂诊断日志${NC}"
else
    echo -e "${YELLOW}⚠️  main.go 缺少诊断日志（可选）${NC}"
fi
echo ""

echo "✅ 所有配置检查通过！"
echo ""
echo "📋 下一步验证步骤："
echo "-----------------------------------"
echo "1. 本地测试（test-a2）："
echo "   make test-a2"
echo "   curl -s http://localhost:8092/api/status | jq '.strategy'"
echo ""
echo "2. Docker 测试（a2）："
echo "   make a2"
echo "   docker logs web3-demo2-app | grep 'StrategyFactory'"
echo "   curl -s http://localhost:8082/api/status | jq '.strategy'"
echo ""
echo "预期结果：两种方式都应该返回 'EPHEMERAL_ANVIL'"
echo ""
