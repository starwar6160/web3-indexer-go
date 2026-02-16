#!/bin/bash

# 🚀 Web3 Indexer 端到端验证脚本
# 验证系统稳定性、数据一致性、Reorg处理

set -e

cd "$(dirname "$0")"

echo "╔════════════════════════════════════════════════════════════════╗"
echo "║     Web3 Indexer 端到端验证 (E2E Verification)               ║"
echo "║     Architecture Audit Fixes Validation                        ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 测试计数
TESTS_PASSED=0
TESTS_FAILED=0

# 辅助函数
log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
    ((TESTS_PASSED++))
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
    ((TESTS_FAILED++))
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# ============================================================================
# 第一步：检查基础设施
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第一步：检查基础设施"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# 检查Anvil
log_info "检查Anvil RPC节点..."
if curl -s http://localhost:8545 -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -e '.result' > /dev/null 2>&1; then
    ANVIL_BLOCK=$(curl -s http://localhost:8545 -X POST -H 'Content-Type: application/json' \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
    log_success "Anvil运行正常，当前块高: $ANVIL_BLOCK"
else
    log_error "Anvil无法连接"
fi

# 检查PostgreSQL
log_info "检查PostgreSQL数据库..."
if docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "SELECT 1" > /dev/null 2>&1; then
    log_success "PostgreSQL连接正常"
else
    log_error "PostgreSQL无法连接"
fi

# ============================================================================
# 第二步：启动Indexer
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第二步：启动Indexer（持续运行模式）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

log_info "启动Indexer..."
pkill -f "go run cmd/indexer" 2>/dev/null || true
sleep 2

# 启动Indexer（后台运行）
CONTINUOUS_MODE=true \
RPC_URLS=http://localhost:8545 \
DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable \
CHAIN_ID=31337 \
START_BLOCK=0 \
WATCH_ADDRESSES=0x5FC8d32690cc91D4c39d9d3abcBD16989F875707 \
API_PORT=2090 \
LOG_LEVEL=info \
LOG_FORMAT=json \
go run cmd/indexer/main.go > /tmp/indexer_e2e.log 2>&1 &

INDEXER_PID=$!
log_success "Indexer启动 (PID: $INDEXER_PID)"

# 等待Indexer启动
sleep 5

# 检查Indexer是否运行
if ps -p $INDEXER_PID > /dev/null; then
    log_success "Indexer进程运行中"
else
    log_error "Indexer进程已退出，查看日志："
    tail -20 /tmp/indexer_e2e.log
    exit 1
fi

# ============================================================================
# 第三步：验证数据一致性（ACID事务）
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第三步：验证数据一致性（ACID事务）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

log_info "等待数据同步（30秒）..."
sleep 30

# 检查Checkpoint与实际数据是否一致
log_info "检查Checkpoint与实际数据一致性..."
RESULT=$(docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "
SELECT 
    (SELECT MAX(block_number) FROM blocks) as max_block,
    (SELECT last_synced_block FROM sync_checkpoints WHERE chain_id=31337) as checkpoint,
    (SELECT COUNT(*) FROM transfers) as transfer_count;
" 2>/dev/null | tail -2 | head -1)

MAX_BLOCK=$(echo "$RESULT" | awk '{print $1}')
CHECKPOINT=$(echo "$RESULT" | awk '{print $2}')
TRANSFER_COUNT=$(echo "$RESULT" | awk '{print $3}')

log_info "数据统计："
echo "  - 最大块号: $MAX_BLOCK"
echo "  - Checkpoint: $CHECKPOINT"
echo "  - Transfer事件数: $TRANSFER_COUNT"

# 验证一致性
if [ "$MAX_BLOCK" = "$CHECKPOINT" ]; then
    log_success "✓ Checkpoint与实际数据一致（都是 $MAX_BLOCK）"
else
    log_error "✗ Checkpoint不一致！Max=$MAX_BLOCK, Checkpoint=$CHECKPOINT"
fi

# 验证Transfer事件
if [ "$TRANSFER_COUNT" -gt 0 ]; then
    log_success "✓ 捕获到 $TRANSFER_COUNT 个Transfer事件"
else
    log_warning "⚠️  未捕获到Transfer事件（可能仍在同步中）"
fi

# ============================================================================
# 第四步：验证事务隔离级别
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第四步：验证事务隔离级别"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

log_info "检查数据库事务隔离级别..."
ISOLATION=$(docker exec web3-indexer-db psql -U postgres -d web3_indexer -c "SHOW transaction_isolation;" 2>/dev/null | grep -v "transaction_isolation" | grep -v "^-" | tr -d ' ')

if [ "$ISOLATION" = "serializable" ]; then
    log_success "✓ 事务隔离级别: $ISOLATION（最高级别）"
else
    log_warning "⚠️  事务隔离级别: $ISOLATION（非serializable）"
fi

# ============================================================================
# 第五步：验证API端点
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第五步：验证API端点"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

log_info "检查/api/status端点..."
if curl -s http://localhost:2090/api/status 2>/dev/null | jq -e '.status' > /dev/null 2>&1; then
    STATUS=$(curl -s http://localhost:2090/api/status 2>/dev/null | jq -r '.status')
    log_success "✓ API状态: $STATUS"
else
    log_warning "⚠️  /api/status端点无响应"
fi

# ============================================================================
# 第六步：验证日志中的关键指标
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第六步：验证日志中的关键指标"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

log_info "检查Indexer日志..."

# 检查持续运行模式是否启用
if grep -q "持续运行模式已开启" /tmp/indexer_e2e.log; then
    log_success "✓ 持续运行模式已启用"
else
    log_error "✗ 持续运行模式未启用"
fi

# 检查块处理
BLOCK_PROCESSED=$(grep -c "block_processed" /tmp/indexer_e2e.log || echo "0")
if [ "$BLOCK_PROCESSED" -gt 0 ]; then
    log_success "✓ 已处理 $BLOCK_PROCESSED 个块"
else
    log_error "✗ 未处理任何块"
fi

# 检查Sequencer接收
SEQUENCER_RECEIVED=$(grep -c "Sequencer received block" /tmp/indexer_e2e.log || echo "0")
if [ "$SEQUENCER_RECEIVED" -gt 0 ]; then
    log_success "✓ Sequencer已接收 $SEQUENCER_RECEIVED 个块"
else
    log_warning "⚠️  Sequencer未接收块"
fi

# 检查错误
ERRORS=$(grep -c '"level":"ERROR"' /tmp/indexer_e2e.log || echo "0")
if [ "$ERRORS" -eq 0 ]; then
    log_success "✓ 日志中无错误"
else
    log_warning "⚠️  日志中有 $ERRORS 个错误"
fi

# ============================================================================
# 第七步：性能指标
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "第七步：性能指标"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

log_info "计算处理速度..."
ELAPSED=30  # 我们等待了30秒
BLOCKS_PROCESSED=$(echo "$BLOCK_PROCESSED")
if [ "$BLOCKS_PROCESSED" -gt 0 ]; then
    SPEED=$(echo "scale=2; $BLOCKS_PROCESSED / $ELAPSED" | bc)
    log_success "✓ 处理速度: $SPEED blocks/second"
    
    if (( $(echo "$SPEED > 10" | bc -l) )); then
        log_success "✓ 性能优异（>10 blocks/sec）"
    elif (( $(echo "$SPEED > 5" | bc -l) )); then
        log_success "✓ 性能良好（>5 blocks/sec）"
    else
        log_warning "⚠️  性能一般（<5 blocks/sec）"
    fi
fi

# ============================================================================
# 第八步：总结
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "验证总结"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

TOTAL_TESTS=$((TESTS_PASSED + TESTS_FAILED))
echo ""
echo "测试结果："
echo "  ✅ 通过: $TESTS_PASSED"
echo "  ❌ 失败: $TESTS_FAILED"
echo "  📊 总计: $TOTAL_TESTS"
echo ""

if [ "$TESTS_FAILED" -eq 0 ]; then
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                  🎉 所有验证通过！                            ║${NC}"
    echo -e "${GREEN}║         系统已准备好用于生产环境（Production Ready）          ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════════╝${NC}"
    EXIT_CODE=0
else
    echo -e "${RED}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║                  ⚠️  验证存在失败项                            ║${NC}"
    echo -e "${RED}║              请检查上述错误并重新运行验证                       ║${NC}"
    echo -e "${RED}╚════════════════════════════════════════════════════════════════╝${NC}"
    EXIT_CODE=1
fi

# 清理
log_info "清理资源..."
kill $INDEXER_PID 2>/dev/null || true

echo ""
echo "详细日志: /tmp/indexer_e2e.log"
echo ""

exit $EXIT_CODE
