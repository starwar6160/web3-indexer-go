#!/bin/bash
# 查看Indexer最新日志 - 专为LLM分析优化

LOG_FILE="./logs/indexer.log"

# 检查日志文件是否存在
if [ ! -f "$LOG_FILE" ]; then
    echo "❌ 日志文件不存在: $LOG_FILE"
    echo "🔍 正在查找可能的日志文件..."
    ls -la ./logs/ || echo "❌ 日志目录也不存在"
    exit 1
fi

echo "=================================="
echo "📋 Indexer最新日志 (最新50条)"
echo "=================================="
tail -50 "$LOG_FILE"

echo ""
echo "=================================="
echo "📦 最新区块处理日志"
echo "=================================="
tail -200 "$LOG_FILE" | grep -E "Sequencer received block|block_processed|Processing block" | tail -20

echo ""
echo "=================================="
echo "⚠️  错误和警告"
echo "=================================="
tail -500 "$LOG_FILE" | grep -iE "error|warn|panic" | tail -10 || echo "✅ 无错误或警告"

echo ""
echo "=================================="
echo "📊 同步状态日志"
echo "=================================="
tail -500 "$LOG_FILE" | grep -E "blocks_scheduled|sync_lag|latest_block" | tail -10
