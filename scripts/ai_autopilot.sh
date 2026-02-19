#!/bin/bash

# --- 🚀 Yokohama Lab AI Autopilot ---
# This script runs the segmented integration tests and provides structured context for AI debugging.

LOG_FILE="tmp/pipeline_results.log"
mkdir -p tmp

echo "🔍 Starting Full-Cycle Data Pipeline Verification..."
echo "----------------------------------------------------"

# 1. 运行分段集成测试
# 使用 -run Stage 参数只运行管道测试
go test -v -tags=integration ./internal/engine -run TestStage > "$LOG_FILE" 2>&1

if [ $? -eq 0 ]; then
    echo "✅ [SUCCESS] All Data Pipeline Stages are logical and healthy."
    exit 0
else
    echo "❌ [FAILURE] Pipeline breakage detected!"
    echo ""
    echo "🚨 AI_FIX_REQUIRED SIGNALS FOUND:"
    grep "AI_FIX_REQUIRED" "$LOG_FILE"
    echo ""
    echo "💡 Suggestion: Provide the contents of $LOG_FILE to your AI coding assistant."
    exit 1
fi
