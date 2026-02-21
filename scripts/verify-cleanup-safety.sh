#!/bin/bash
set -e

echo "🔍 Docker 清理前安全检查"
echo "================================================================"
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查函数
check_status() {
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ $1${NC}"
        return 0
    else
        echo -e "${RED}❌ $1${NC}"
        return 1
    fi
}

# 1. 检查 Docker 是否运行
echo "📋 检查 1/7：Docker 服务状态"
if docker info > /dev/null 2>&1; then
    echo -e "${GREEN}   ✅ Docker 服务运行中${NC}"
else
    echo -e "${RED}   ❌ Docker 服务未运行${NC}"
    exit 1
fi

# 2. 列出所有 web3- 容器
echo ""
echo "📋 检查 2/7：所有 web3- 容器"
CONTAINER_COUNT=$(docker ps -a --filter "name=web3-" --format '{{.Names}}' | wc -l)
echo "   发现 $CONTAINER_COUNT 个 web3- 容器："
docker ps -a --filter "name=web3-" --format '   {{.Names}}\t{{.Status}}\t{{.Ports}}'

# 3. 检查当前 Demo2 的 project name
echo ""
echo "📋 检查 3/7：当前 web3-indexer-app 的项目标识"
CURRENT_PROJECT=$(docker inspect web3-indexer-app --format '{{index .Config.Labels "com.docker.compose.project"}}' 2>/dev/null || echo "unknown")
echo "   COMPOSE_PROJECT_NAME: $CURRENT_PROJECT"

if [ "$CURRENT_PROJECT" = "indexer-demo" ]; then
    echo -e "${YELLOW}   ⚠️  警告：当前容器已使用 indexer-demo，可能已执行过清理${NC}"
elif [ "$CURRENT_PROJECT" = "web3-indexer" ]; then
    echo -e "${GREEN}   ✅ 确认：使用默认 project name（需要清理）${NC}"
else
    echo -e "${YELLOW}   ⚠️  未知 project name：$CURRENT_PROJECT${NC}"
fi

# 4. 检查 indexer-demo 容器
echo ""
echo "📋 检查 4/7：检查是否已存在 indexer-demo-* 容器"
DEMO_CONTAINERS=$(docker ps -a --filter "name=indexer-demo" --format '{{.Names}}' || true)
if [ -n "$DEMO_CONTAINERS" ]; then
    echo -e "${YELLOW}   ⚠️  发现 indexer-demo 容器：${NC}"
    echo "$DEMO_CONTAINERS"
    echo "   注意：清理脚本可能已执行过"
else
    echo -e "${GREEN}   ✅ 无 indexer-demo 容器（正常状态）${NC}"
fi

# 5. 端口占用检查
echo ""
echo "📋 检查 5/7：端口占用情况"
echo "   关键端口：8081 (Testnet), 8082 (Demo2), 8545 (Anvil)"
if command -v netstat > /dev/null 2>&1; then
    netstat -tulpn 2>/dev/null | grep -E ':(8081|8082|8545)' | head -10 || echo "   无端口占用（netstat）"
elif command -v ss > /dev/null 2>&1; then
    ss -tulpn 2>/dev/null | grep -E ':(8081|8082|8545)' | head -10 || echo "   无端口占用（ss）"
else
    echo "   ⚠️  无法检查端口（netstat/ss 不可用）"
fi

# 6. API 服务检查
echo ""
echo "📋 检查 6/7：API 服务健康度"
DEMO2_UP=0
TESTNET_UP=0

if curl -s --max-time 2 http://localhost:8082/api/status > /dev/null 2>&1; then
    echo -e "${GREEN}   ✅ Demo2 API (8082): 运行中${NC}"
    DEMO2_UP=1
else
    echo -e "${RED}   ❌ Demo2 API (8082): 无法连接${NC}"
fi

if curl -s --max-time 2 http://localhost:8081/api/status > /dev/null 2>&1; then
    echo -e "${GREEN}   ✅ Testnet API (8081): 运行中${NC}"
    TESTNET_UP=1
else
    echo -e "${YELLOW}   ⚠️  Testnet API (8081): 无法连接${NC}"
fi

# 7. 数据库连接检查
echo ""
echo "📋 检查 7/7：数据库连接"
DB_CONNECTED=0

if docker exec web3-indexer-db pg_isready -U postgres > /dev/null 2>&1; then
    echo -e "${GREEN}   ✅ web3-indexer-db: 连接正常${NC}"
    DB_CONNECTED=1
elif docker exec indexer-demo-db pg_isready -U postgres > /dev/null 2>&1; then
    echo -e "${GREEN}   ✅ indexer-demo-db: 连接正常${NC}"
    DB_CONNECTED=1
else
    echo -e "${YELLOW}   ⚠️  无法连接到数据库容器${NC}"
fi

# 总结和建议
echo ""
echo "================================================================"
echo "📊 安全检查总结"
echo "================================================================"

RISK_LEVEL="LOW"
if [ "$CURRENT_PROJECT" = "web3-indexer" ] && [ $DEMO2_UP -eq 1 ] && [ $TESTNET_UP -eq 1 ]; then
    RISK_LEVEL="LOW"
    echo -e "${GREEN}✅ 风险等级：低${NC}"
    echo "   - Demo2 服务正常，可以安全重启"
    echo "   - Testnet 服务正常，不会被影响"
elif [ "$CURRENT_PROJECT" = "indexer-demo" ]; then
    RISK_LEVEL="NONE"
    echo -e "${GREEN}✅ 风险等级：无${NC}"
    echo "   - 容器已使用 indexer-demo，可能已执行过清理"
    echo "   - 建议跳过清理脚本"
else
    RISK_LEVEL="MEDIUM"
    echo -e "${YELLOW}⚠️  风险等级：中${NC}"
    echo "   - 部分服务可能未运行"
    echo "   - 建议检查服务状态后再执行清理"
fi

echo ""
echo "🎯 建议操作："
if [ "$RISK_LEVEL" = "NONE" ]; then
    echo "   跳过清理脚本，环境已正确配置"
elif [ "$RISK_LEVEL" = "LOW" ]; then
    echo "   执行清理脚本: bash scripts/cleanup-docker-orphans.sh"
    echo ""
    echo "   预期结果："
    echo "   - 停止并删除 web3-indexer-* 容器"
    echo "   - 创建 indexer-demo-* 容器"
    echo "   - Demo2 API (8082) 将短暂中断（~10 秒）"
    echo "   - Testnet API (8081) 不受影响"
else
    echo "   检查服务状态，解决以下问题："
    if [ $DEMO2_UP -eq 0 ]; then
        echo "   - Demo2 API (8082) 未运行"
    fi
    if [ $TESTNET_UP -eq 0 ]; then
        echo "   - Testnet API (8081) 未运行（可选）"
    fi
fi

echo ""
echo "================================================================"
