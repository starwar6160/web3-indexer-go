#!/bin/bash

# 🔍 Web3 Indexer - 端口验证和独立命令指南
# 本脚本提供逐步验证每个组件的独立命令

set -e

echo "🔍 Web3 Indexer - 端口配置验证"
echo "=================================="
echo ""

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 端口配置
DB_PORT=15433
ANVIL_PORT=8546
API_PORT=8088

echo -e "${BLUE}📋 端口配置:${NC}"
echo "  PostgreSQL: localhost:$DB_PORT"
echo "  Anvil RPC:  localhost:$ANVIL_PORT"
echo "  API Server: localhost:$API_PORT"
echo ""

# 步骤 A: 清理旧进程
echo -e "${BLUE}步骤 A: 清理旧进程${NC}"
echo "命令: make stop"
echo ""
echo "验证:"
echo "  - 确保没有 bin/indexer 进程运行"
echo "  - 确保 Docker 容器已停止"
echo ""

# 步骤 B: 启动 Anvil 和 PostgreSQL
echo -e "${BLUE}步骤 B: 启动 Anvil 和 PostgreSQL${NC}"
echo "命令: make anvil-up"
echo ""
echo "预期输出:"
echo "  ✅ Anvil已启动"
echo "  ⛽️  RPC URL: http://localhost:$ANVIL_PORT"
echo "  🔗 Chain ID: 31337"
echo ""

# 步骤 C: 验证 Anvil 连接
echo -e "${BLUE}步骤 C: 验证 Anvil 连接${NC}"
echo "命令:"
echo "  curl -X POST http://localhost:$ANVIL_PORT \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"jsonrpc\":\"2.0\",\"method\":\"eth_chainId\",\"params\":[],\"id\":1}'"
echo ""
echo "预期响应:"
echo "  {\"jsonrpc\":\"2.0\",\"result\":\"0x7a69\",\"id\":1}"
echo "  (0x7a69 = 31337 in hex)"
echo ""

# 步骤 D: 验证 PostgreSQL 连接
echo -e "${BLUE}步骤 D: 验证 PostgreSQL 连接${NC}"
echo "命令:"
echo "  docker exec web3-indexer-db pg_isready -U postgres"
echo ""
echo "预期响应:"
echo "  accepting connections"
echo ""

# 步骤 E: 部署演示合约
echo -e "${BLUE}步骤 E: 部署演示合约${NC}"
echo "命令: make demo-deploy"
echo ""
echo "预期输出:"
echo "  ✅ Connected to Anvil (Chain ID: 31337)"
echo "  📝 Deploying from: 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
echo "  🚀 Deploying ERC20 contract..."
echo "  ✅ Contract deployed at: 0x..."
echo "  📤 Sending test transactions..."
echo "  ✅ TX 1 sent: 0x..."
echo "  ... (10 transactions total)"
echo ""

# 步骤 F: 编译索引器
echo -e "${BLUE}步骤 F: 编译索引器${NC}"
echo "命令: make build"
echo ""
echo "预期输出:"
echo "  ✅ 编译完成: bin/indexer"
echo ""

# 步骤 G: 启动索引器
echo -e "${BLUE}步骤 G: 启动索引器${NC}"
echo "命令:"
echo "  DATABASE_URL=postgres://postgres:postgres@localhost:$DB_PORT/indexer?sslmode=disable \\"
echo "  RPC_URLS=http://localhost:$ANVIL_PORT \\"
echo "  CHAIN_ID=31337 \\"
echo "  START_BLOCK=0 \\"
echo "  LOG_LEVEL=debug \\"
echo "  ./bin/indexer"
echo ""
echo "预期日志:"
echo "  ✅ configuration_loaded"
echo "  ✅ database_connected"
echo "  ✅ rpc_pool_initialized"
echo "  ✅ blocks_scheduled"
echo "  ✅ sequencer_started"
echo "  ✅ smart_sleep_system_enabled"
echo ""

# 步骤 H: 验证健康检查
echo -e "${BLUE}步骤 H: 验证健康检查${NC}"
echo "命令:"
echo "  curl http://localhost:$API_PORT/healthz | jq ."
echo ""
echo "预期响应:"
echo "  {"
echo "    \"status\": \"healthy\","
echo "    \"timestamp\": \"...\","
echo "    \"checks\": {"
echo "      \"database\": {\"status\": \"healthy\", ...},"
echo "      \"rpc\": {\"status\": \"healthy\", ...},"
echo "      \"sequencer\": {\"status\": \"healthy\", ...},"
echo "      \"fetcher\": {\"status\": \"healthy\", ...}"
echo "    }"
echo "  }"
echo ""

# 步骤 I: 验证加密身份 (EdDSA)
echo -e "${BLUE}步骤 I: 验证加密身份 (EdDSA)${NC}"
echo "命令:"
echo "  gpg --verify README.md.asc README.md"
echo ""
echo "预期响应:"
echo "  gpg: Good signature from \"Zhou Wei <zhouwei6160@gmail.com>\""
echo "  gpg: Primary key fingerprint: FFA0 B998 E7AF 2A9A 9A2C  6177 F965 25FE 5857 5DCF"
echo ""

# 完整工作流
echo -e "${YELLOW}=== 完整工作流 ===${NC}"
echo ""
echo "1️⃣  清理环境:"
echo "   make stop"
echo ""
echo "2️⃣  启动基础设施:"
echo "   make anvil-up"
echo ""
echo "3️⃣  验证 Anvil:"
echo "   curl -X POST http://localhost:$ANVIL_PORT -H 'Content-Type: application/json' -d '{\"jsonrpc\":\"2.0\",\"method\":\"eth_chainId\",\"params\":[],\"id\":1}'"
echo ""
echo "4️⃣  部署合约:"
echo "   make demo-deploy"
echo ""
echo "5️⃣  编译索引器:"
echo "   make build"
echo ""
echo "6️⃣  启动索引器 (在另一个终端):"
echo "   DATABASE_URL=postgres://postgres:postgres@localhost:$DB_PORT/indexer?sslmode=disable RPC_URLS=http://localhost:$ANVIL_PORT CHAIN_ID=31337 START_BLOCK=0 LOG_LEVEL=debug ./bin/indexer"
echo ""
echo "7️⃣  验证健康状态 (在第三个终端):"
echo "   curl http://localhost:$API_PORT/healthz | jq ."
echo ""
echo "8️⃣  停止所有服务:"
echo "   make stop"
echo ""

echo -e "${GREEN}✨ 验证指南完成${NC}"
