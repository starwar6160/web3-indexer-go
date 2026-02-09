#!/bin/bash
# 查看Indexer最新日志 - 专为LLM分析优化

echo "=================================="
echo "📋 Indexer最新日志 (最新50条)"
echo "=================================="
tail -50 /tmp/indexer.log

echo ""
echo "=================================="
echo "📦 最新区块处理日志"
echo "=================================="
tail -200 /tmp/indexer.log | grep -E "Sequencer received block|block_processed|Processing block" | tail -20

echo ""
echo "=================================="
echo "⚠️  错误和警告"
echo "=================================="
tail -500 /tmp/indexer.log | grep -iE "error|warn|panic" | tail -10 || echo "✅ 无错误或警告"

echo ""
echo "=================================="
echo "📊 同步状态日志"
echo "=================================="
tail -500 /tmp/indexer.log | grep -E "blocks_scheduled|sync_lag|latest_block" | tail -10
