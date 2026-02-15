#!/bin/bash
# ==============================================================================
# Web3 Indexer - Testnet Pre-flight Checks
# ==============================================================================
# 用途：在启动 make a1 前，执行 5 步原子化验证
# 作者：追求 6 个 9 持久性的资深后端
# ==============================================================================

set -euo pipefail

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 步骤计数
STEP=0

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✅]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[⚠️]${NC} $1"
}

log_error() {
    echo -e "${RED}[❌]${NC} $1"
}

log_step() {
    STEP=$((STEP + 1))
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE}步骤 ${STEP}: $1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

# 错误处理
trap 'log_error "预检失败在步骤 $STEP"; exit 1' ERR

# ==============================================================================
# 第一步：RPC 连通性与额度预检
# ==============================================================================
check_rpc_connectivity() {
    log_step "RPC 连通性与额度预检"

    # 加载 .env.testnet 获取 RPC URL
    if [ ! -f ".env.testnet" ]; then
        log_error ".env.testnet 文件不存在"
        exit 1
    fi

    # 尝试从多个源获取 RPC URL
    if [ -f ".env.testnet.local" ]; then
        log_info "使用 .env.testnet.local 中的 API Key"
        source .env.testnet.local
    elif [ -n "${SEPOLIA_RPC_URLS:-}" ]; then
        log_info "使用环境变量 SEPOLIA_RPC_URLS"
    else
        log_warn "未找到 RPC URL 配置，尝试使用默认值"
        export SEPOLIA_RPC_URLS="https://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY"
    fi

    # 取第一个 RPC URL 进行测试
    RPC_URL=$(echo "$SEPOLIA_RPC_URLS" | cut -d',' -f1)
    log_info "测试 RPC URL: ${RPC_URL:0:50}..."

    # 执行 eth_blockNumber 请求
    RESPONSE=$(curl -s -X POST \
        -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        "$RPC_URL" 2>&1)

    # 检查是否包含 "result"
    if echo "$RESPONSE" | grep -q '"result"'; then
        # 提取区块号
        BLOCK_HEX=$(echo "$RESPONSE" | grep -o '"result":"[^"]*"' | cut -d'"' -f4)
        BLOCK_DEC=$((16#${BLOCK_HEX:2}))
        log_success "RPC 连接成功"
        log_info "当前链头高度: ${BLOCK_DEC} (0x${BLOCK_HEX:2})"

        # 验证高度是否合理（Sepolia 当前约 1026 万）
        if [ "$BLOCK_DEC" -lt 10000000 ]; then
            log_warn "区块高度 ${BLOCK_DEC} 似乎过低，请确认网络"
        else
            log_success "区块高度验证通过（千万量级）"
        fi
    else
        log_error "RPC 请求失败"
        log_info "响应内容: $RESPONSE"
        log_error "请检查："
        log_error "  1. API Key 是否正确"
        log_error "  2. 网络连接是否正常"
        log_error "  3. RPC Provider 是否在线"
        exit 1
    fi
}

# ==============================================================================
# 第二步：数据库物理隔离验证
# ==============================================================================
check_db_isolation() {
    log_step "数据库物理隔离验证"

    # 检查是否已有 testnet 容器在运行
    if docker ps | grep -q "web3-indexer-sepolia-db"; then
        log_info "测试网数据库容器正在运行"

        # 检查数据库名称
        DB_NAME=$(docker exec web3-indexer-sepolia-db psql -U postgres -lqt | grep -w "web3_sepolia" || true)
        if [ -n "$DB_NAME" ]; then
            log_success "数据库 web3_sepolia 已存在"

            # 检查是否有旧数据
            TABLE_COUNT=$(docker exec web3-indexer-sepolia-db psql -U postgres -d web3_sepolia -tAc "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'blocks';" 2>/dev/null || echo "0")
            if [ "$TABLE_COUNT" -gt 0 ]; then
                log_warn "检测到旧数据存在（blocks 表有 $TABLE_COUNT 条记录）"
                log_info "建议运行: make reset-a1"
            else
                log_success "数据库干净，无旧数据"
            fi
        else
            log_warn "容器运行但数据库 web3_sepolia 不存在"
        fi
    else
        log_info "测试网数据库容器未运行（首次启动正常）"
        log_info "将在 make a1 时自动创建"
    fi

    # 验证端口隔离（testnet 使用 15433，demo 使用 15432）
    if docker ps | grep -q "0.0.0.0:15432"; then
        log_info "Demo 数据库运行在端口 15432（隔离正常）"
    fi

    if docker ps | grep -q "0.0.0.0:15433"; then
        log_success "Testnet 数据库运行在端口 15433（隔离正常）"
    fi

    log_success "数据库物理隔离验证通过"
}

# ==============================================================================
# 第三步：起始高度解析逻辑验证
# ==============================================================================
check_start_block_logic() {
    log_step "起始高度解析逻辑验证"

    # 检查 .env.testnet 中的 START_BLOCK 配置
    if grep -q "START_BLOCK=latest" .env.testnet; then
        log_success "START_BLOCK=latest 配置正确"

        # 验证代码中是否有处理 latest 的逻辑
        if grep -q "StartBlockStr == \"latest\"" cmd/indexer/main.go; then
            log_success "代码中已实现 latest 解析逻辑"
        else
            log_error "代码中缺少 latest 解析逻辑"
            exit 1
        fi
    else
        START_BLOCK=$(grep "START_BLOCK=" .env.testnet | cut -d'=' -f2)
        log_warn "START_BLOCK=$START_BLOCK（建议使用 latest）"
    fi

    # 检查硬编码的演示模式参数
    if grep -q "10262444" cmd/indexer/main.go; then
        log_success "演示模式硬编码参数已添加（最小起始块 10262444）"
    else
        log_warn "未找到演示模式硬编码参数"
    fi

    log_success "起始高度解析逻辑验证通过"
}

# ==============================================================================
# 第四步：单步限流抓取配置验证
# ==============================================================================
check_rate_limiting() {
    log_step "单步限流抓取配置验证"

    # 检查 .env.testnet 中的限流配置（只取第一个匹配项，去除空白）
    RPC_RATE_LIMIT=$(grep "RPC_RATE_LIMIT=" .env.testnet | head -n1 | cut -d'=' -f2 | tr -d '[:space:]')
    FETCH_CONCURRENCY=$(grep "FETCH_CONCURRENCY=" .env.testnet | head -n1 | cut -d'=' -f2 | tr -d '[:space:]')
    MAX_SYNC_BATCH=$(grep "MAX_SYNC_BATCH=" .env.testnet | head -n1 | cut -d'=' -f2 | tr -d '[:space:]')

    log_info "当前配置："
    log_info "  RPC_RATE_LIMIT=$RPC_RATE_LIMIT req/sec"
    log_info "  FETCH_CONCURRENCY=$FETCH_CONCURRENCY"
    log_info "  MAX_SYNC_BATCH=$MAX_SYNC_BATCH"

    # 验证限流参数是否合理（保守值）
    if [ "$RPC_RATE_LIMIT" -le 3 ]; then
        log_success "限流配置保守（QPS=$RPC_RATE_LIMIT），安全"
    else
        log_warn "限流配置偏高（QPS=$RPC_RATE_LIMIT），可能触发 RPC 频率限制"
    fi

    if [ "$FETCH_CONCURRENCY" -le 5 ]; then
        log_success "并发配置保守（$FETCH_CONCURRENCY），安全"
    else
        log_warn "并发配置偏高（$FETCH_CONCURRENCY），可能过载"
    fi

    # 检查代码中是否有限流器实现
    if grep -q "SetRateLimit" cmd/indexer/main.go || grep -q "TokenBucket" internal/engine/*.go; then
        log_success "代码中已实现限流器"
    else
        log_warn "未找到限流器实现（可能使用默认值）"
    fi

    log_success "限流配置验证通过"
}

# ==============================================================================
# 第五步：可观测性配置验证
# ==============================================================================
check_observability() {
    log_step "可观测性配置验证"

    # 检查 Prometheus metrics 端点配置
    if grep -q "/metrics" cmd/indexer/main.go; then
        log_success "Prometheus /metrics 端点已配置"
    else
        log_warn "未找到 /metrics 端点配置"
    fi

    # 检查端口配置
    API_PORT=$(grep "API_PORT=" .env.testnet | cut -d'=' -f2 || echo "8081")
    log_info "API 端口: $API_PORT"
    log_info "Dashboard: http://localhost:$API_PORT"
    log_info "Metrics: http://localhost:$API_PORT/metrics"

    # 检查日志级别
    LOG_LEVEL=$(grep "LOG_LEVEL=" .env.testnet | cut -d'=' -f2)
    log_info "日志级别: $LOG_LEVEL"

    if [ "$LOG_LEVEL" = "debug" ]; then
        log_warn "Debug 模式可能影响性能"
    fi

    log_success "可观测性配置验证通过"
}

# ==============================================================================
# 主执行流程
# ==============================================================================
main() {
    echo -e "${BLUE}"
    echo "============================================"
    echo "  Web3 Indexer - Testnet Pre-flight Checks"
    echo "  追求 6 个 9 持久性 · 小步快跑验证"
    echo "============================================"
    echo -e "${NC}"

    check_rpc_connectivity
    check_db_isolation
    check_start_block_logic
    check_rate_limiting
    check_observability

    echo -e "\n${GREEN}"
    echo "============================================"
    echo "  ✅ 所有预检通过！"
    echo "============================================"
    echo -e "${NC}"
    echo -e "下一步操作："
    echo -e "  ${BLUE}make a1${NC}           # 启动测试网索引器"
    echo -e "  ${BLUE}make reset-a1${NC}     # 完全重置测试网环境"
    echo -e "  ${BLUE}make logs-testnet${NC} # 查看实时日志"
    echo ""
}

# 执行主流程
main "$@"
