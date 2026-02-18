#!/bin/bash
# 协程堆栈跟踪调试工具
# 用于定位死锁和协程泄露问题

set -e

# 配置
PID_FILE="/tmp/indexer.pid"
OUTPUT_DIR="/tmp/indexer_debug"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Web3 Indexer 协程堆栈跟踪调试工具                    ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

# 查找进程 PID
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    echo -e "${GREEN}✅ 从 PID 文件读取: $PID${NC}"
else
    # 尝试从进程名查找
    PID=$(pgrep -f "indexer" | head -1)
    if [ -z "$PID" ]; then
        echo -e "${RED}❌ 未找到 indexer 进程${NC}"
        echo -e "${YELLOW}💡 请先启动 indexer: docker-compose up -d web3-indexer-app${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ 从进程名查找: $PID${NC}"
fi

echo -e "${BLUE}📊 正在收集调试信息...${NC}"
echo ""

# 1. 协程堆栈跟踪
echo -e "${YELLOW}[1/6] 获取协程堆栈跟踪...${NC}"
OUTPUT_FILE="$OUTPUT_DIR/stack_trace_$TIMESTAMP.txt"
if command -v gtrace &> /dev/null; then
    gtrace $PID > "$OUTPUT_FILE" 2>&1
    echo -e "${GREEN}✅ 堆栈跟踪已保存: $OUTPUT_FILE${NC}"
else
    # 使用 pprof 备选方案
    echo -e "${YELLOW}⚠️  gtrace 未安装，使用 pprof...${NC}"
    go tool pprof http://localhost:8080/debug/pprof/goroutine > "$OUTPUT_FILE" 2>&1 || true
    echo -e "${GREEN}✅ Goroutine 信息已保存: $OUTPUT_FILE${NC}"
fi

# 2. 协程数量统计
echo -e "${YELLOW}[2/6] 统计协程数量...${NC}"
GOROUTINE_COUNT=$(curl -s http://localhost:8080/debug/pprof/goroutine?debug=1 | grep -c "goroutine" || echo "0")
echo -e "${GREEN}✅ 当前协程数量: $GOROUTINE_COUNT${NC}"

# 3. 内存使用情况
echo -e "${YELLOW}[3/6] 获取内存使用情况...${NC}"
HEAP_FILE="$OUTPUT_DIR/heap_$TIMESTAMP.txt"
curl -s http://localhost:8080/debug/pprof/heap > "$HEAP_FILE" 2>&1 || true
echo -e "${GREEN}✅ Heap 信息已保存: $HEAP_FILE${NC}"

# 4. 阻塞协程检测
echo -e "${YELLOW}[4/6] 检测阻塞协程...${NC}"
BLOCK_FILE="$OUTPUT_DIR/blocked_$TIMESTAMP.txt"
curl -s "http://localhost:8080/debug/pprof/goroutine?debug=2" | grep -A 10 "semacquire" > "$BLOCK_FILE" 2>&1 || true
if [ -s "$BLOCK_FILE" ]; then
    echo -e "${RED}⚠️  发现阻塞协程！详情: $BLOCK_FILE${NC}"
else
    echo -e "${GREEN}✅ 无阻塞协程${NC}"
fi

# 5. 死锁检测
echo -e "${YELLOW}[5/6] 死锁检测...${NC}"
DEADLOCK_FILE="$OUTPUT_DIR/deadlock_$TIMESTAMP.txt"
curl -s "http://localhost:8080/debug/pprof/mutex?debug=1" > "$DEADLOCK_FILE" 2>&1 || true
echo -e "${GREEN}✅ Mutex 信息已保存: $DEADLOCK_FILE${NC}"

# 6. 系统状态快照
echo -e "${YELLOW}[6/6] 获取系统状态快照...${NC}"
STATUS_FILE="$OUTPUT_DIR/status_$TIMESTAMP.json"
curl -s http://localhost:8080/api/status > "$STATUS_FILE" 2>&1 || true
echo -e "${GREEN}✅ 系统状态已保存: $STATUS_FILE${NC}"

# 分析结果
echo ""
echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                    分析报告                            ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""

# 检查协程数量
if [ "$GOROUTINE_COUNT" -gt 1000 ]; then
    echo -e "${RED}❌ 协程数量异常: $GOROUTINE_COUNT (>1000)${NC}"
    echo -e "${YELLOW}💡 可能存在协程泄露${NC}"
else
    echo -e "${GREEN}✅ 协程数量正常: $GOROUTINE_COUNT${NC}"
fi

# 检查阻塞协程
if [ -s "$BLOCK_FILE" ]; then
    echo -e "${RED}❌ 发现阻塞协程${NC}"
    echo -e "${YELLOW}💡 查看: cat $BLOCK_FILE${NC}"
else
    echo -e "${GREEN}✅ 无阻塞协程${NC}"
fi

# 检查系统状态
if [ -f "$STATUS_FILE" ]; then
    SYNCED=$(jq -r '.total_blocks // "0"' "$STATUS_FILE")
    LATEST=$(jq -r '.latest_block // "0"' "$STATUS_FILE")
    STATE=$(jq -r '.state // "unknown"' "$STATUS_FILE")
    LAG=$(jq -r '.sync_lag // "0"' "$STATUS_FILE")

    echo -e "${BLUE}📊 系统状态:${NC}"
    echo -e "   - Synced: $SYNCED"
    echo -e "   - Latest: $LATEST"
    echo -e "   - State: $STATE"
    echo -e "   - Lag: $LAG"

    if [ "$STATE" == "sleep" ]; then
        echo -e "${RED}❌ 系统处于休眠状态${NC}"
        echo -e "${YELLOW}💡 这可能是死锁的原因${NC}"
    fi
fi

echo ""
echo -e "${BLUE}📁 所有调试信息已保存到: $OUTPUT_DIR${NC}"
echo -e "${YELLOW}💡 查看堆栈跟踪: cat $OUTPUT_FILE${NC}"
echo ""
