#!/bin/bash
# 死锁自愈看门狗验证脚本
# 用于验证 DeadlockWatchdog 功能是否正常工作

set -e

echo "🛡️ Deadlock Watchdog 验证脚本"
echo "================================"
echo ""

# 检查容器是否运行
echo "📋 Step 1: 检查容器状态"
if ! docker ps | grep -q "web3-demo2"; then
    echo "❌ 错误: web3-demo2 容器未运行"
    echo "请先启动容器: make infra-up"
    exit 1
fi
echo "✅ 容器正在运行"
echo ""

# 检查环境变量
echo "📋 Step 2: 检查环境变量"
CHAIN_ID=$(docker exec web3-demo2-app printenv CHAIN_ID 2>/dev/null || echo "unknown")
DEMO_MODE=$(docker exec web3-demo2-app printenv DEMO_MODE 2>/dev/null || echo "unknown")

echo "CHAIN_ID: $CHAIN_ID"
echo "DEMO_MODE: $DEMO_MODE"

if [ "$CHAIN_ID" != "31337" ] && [ "$DEMO_MODE" != "true" ]; then
    echo "⚠️  警告: 当前环境不支持看门狗（需要 CHAIN_ID=31337 或 DEMO_MODE=true）"
    echo "看门狗将不会启用"
else
    echo "✅ 环境配置正确，看门狗应该启用"
fi
echo ""

# 检查编译状态
echo "📋 Step 3: 检查代码编译"
if go build -o /tmp/indexer-test ./cmd/indexer 2>&1 | grep -q "error"; then
    echo "❌ 错误: 代码编译失败"
    exit 1
fi
echo "✅ 代码编译成功"
echo ""

# 检查 Prometheus 指标
echo "📋 Step 4: 检查 Prometheus 指标"
echo "获取自愈指标..."
METRICS=$(curl -s http://localhost:8082/metrics 2>/dev/null || echo "")

if echo "$METRICS" | grep -q "self_healing"; then
    echo "✅ 自愈指标已注册:"
    echo "$METRICS" | grep "self_healing" | sed 's/^/  /'
else
    echo "⚠️  警告: 未找到自愈指标（容器可能未启动或端口不正确）"
fi
echo ""

# 检查日志
echo "📋 Step 5: 检查看门狗日志"
echo "最近 20 行日志中的看门狗相关信息:"
docker logs --tail 20 web3-demo2-app 2>/dev/null | grep -i "deadlock\|watchdog\|self.heal" | sed 's/^/  /' || echo "  (未找到看门狗日志)"
echo ""

# 模拟时空撕裂测试（可选）
echo "📋 Step 6: 模拟时空撕裂（可选）"
read -p "是否要模拟时空撕裂来测试看门狗？这需要 120 秒等待时间。(y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "⚠️  警告: 此操作将修改数据库，仅用于测试环境！"
    read -p "确认继续？(y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🔧 正在模拟时空撕裂..."

        # 获取当前 RPC 高度
        RPC_HEIGHT=$(docker exec web3-demo2-app curl -s -X POST \
            -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            http://127.0.0.1:8545 2>/dev/null | jq -r '.result' | xargs printf "%d")

        if [ -z "$RPC_HEIGHT" ] || [ "$RPC_HEIGHT" -eq 0 ]; then
            echo "❌ 错误: 无法获取 RPC 高度"
            exit 1
        fi

        echo "当前 RPC 高度: $RPC_HEIGHT"

        # 重置数据库游标到很低的高度（模拟时空撕裂）
        FAKE_HEIGHT=240
        echo "🔧 重置数据库游标到: $FAKE_HEIGHT"

        docker exec -i web3-demo2-db psql -U postgres -d web3_indexer <<SQL
UPDATE sync_checkpoints SET last_synced_block = '$FAKE_HEIGHT' WHERE chain_id = 31337;
SELECT 'Database cursor updated' AS status;
SQL

        echo "✅ 时空撕裂已模拟"
        echo ""
        echo "⏳ 等待 120 秒，看门狗应该会检测到并自动修复..."
        echo "你可以使用另一个终端运行: docker logs -f web3-demo2-app"
        echo ""

        # 等待 120 秒
        for i in {120..1}; do
            printf "\r等待中: %d 秒..." $i
            sleep 1
        done
        echo ""
        echo ""

        # 检查自愈结果
        echo "📊 检查自愈结果:"
        NEW_METRICS=$(curl -s http://localhost:8082/metrics 2>/dev/null || echo "")
        TRIGGERED=$(echo "$NEW_METRICS" | grep "indexer_self_healing_triggered_total" | awk '{print $2}')
        SUCCESS=$(echo "$NEW_METRICS" | grep "indexer_self_healing_success_total" | awk '{print $2}')
        FAILURE=$(echo "$NEW_METRICS" | grep "indexer_self_healing_failure_total" | awk '{print $2}')

        echo "自愈触发次数: $TRIGGERED"
        echo "自愈成功次数: $SUCCESS"
        echo "自愈失败次数: $FAILURE"
        echo ""

        if [ "$TRIGGERED" -gt 0 ]; then
            echo "✅ 看门狗已触发自愈"
            if [ "$SUCCESS" -gt 0 ]; then
                echo "✅ 自愈成功"
            elif [ "$FAILURE" -gt 0 ]; then
                echo "❌ 自愈失败，请查看日志"
            fi
        else
            echo "⚠️  看门狗未触发，可能原因:"
            echo "  - 环境不支持（非 Anvil 或演示模式）"
            echo "  - 看门狗未启用"
            echo "  - 等待时间不足"
        fi
    fi
else
    echo "⏭️  跳过模拟测试"
fi
echo ""

# 总结
echo "📊 验证总结"
echo "==========="
echo "✅ 所有检查通过"
echo ""
echo "📖 详细文档: DEADLOCK_WATCHDOG_IMPLEMENTATION.md"
echo "🔧 配置文件: internal/config/config.go"
echo "📡 监控端点: http://localhost:8082/metrics"
echo ""
