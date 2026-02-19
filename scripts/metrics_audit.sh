#!/bin/bash
# metrics_audit.sh - Grafana/Prometheus 指标诊断脚本
# 自动检测指标断流问题

set -e

echo "🔍 指标系统诊断开始..."
echo ""

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. 检查本地指标端点
echo "📊 步骤 1: 检查本地 /metrics 端点"
if curl -s http://localhost:8082/metrics > /tmp/metrics_output.txt 2>/dev/null; then
    echo -e "${GREEN}✅ 8082 端口可访问${NC}"
    
    # 检查关键指标是否存在
    if grep -q "indexer_blocks_processed_total" /tmp/metrics_output.txt; then
        BLOCKS=$(grep "indexer_blocks_processed_total" /tmp/metrics_output.txt | grep -v "#" | awk '{print $2}' | head -1)
        echo -e "${GREEN}✅ BlocksProcessed: $BLOCKS${NC}"
    else
        echo -e "${RED}❌ BlocksProcessed 指标缺失${NC}"
    fi
    
    if grep -q "indexer_transfers_processed_total" /tmp/metrics_output.txt; then
        TRANSFERS=$(grep "indexer_transfers_processed_total" /tmp/metrics_output.txt | grep -v "#" | awk '{print $2}' | head -1)
        echo -e "${GREEN}✅ TransfersProcessed: $TRANSFERS${NC}"
    else
        echo -e "${RED}❌ TransfersProcessed 指标缺失${NC}"
    fi
    
    if grep -q "indexer_realtime_tps" /tmp/metrics_output.txt; then
        TPS=$(grep "indexer_realtime_tps" /tmp/metrics_output.txt | grep -v "#" | awk '{print $2}' | head -1)
        echo -e "${GREEN}✅ RealtimeTPS: $TPS${NC}"
    else
        echo -e "${RED}❌ RealtimeTPS 指标缺失${NC}"
    fi
else
    echo -e "${RED}❌ 无法访问 localhost:8082/metrics${NC}"
    echo "   请确保 indexer 服务正在运行"
fi
echo ""

# 2. 检查 Prometheus 连通性
echo "📊 步骤 2: 检查 Prometheus 连通性"
if curl -s http://localhost:9090/api/v1/status/targets > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Prometheus (localhost:9090) 可访问${NC}"
    
    # 检查 indexer job 状态
    if curl -s "http://localhost:9090/api/v1/query?query=up{job=~'.*indexer.*'}" | grep -q '"result":\[{"metric"'; then
        echo -e "${GREEN}✅ Prometheus 正在抓取 indexer 指标${NC}"
    else
        echo -e "${YELLOW}⚠️ Prometheus 可能没有配置 indexer job${NC}"
        echo "   检查 prometheus.yml 中的 scrape_configs"
    fi
else
    echo -e "${YELLOW}⚠️ Prometheus (localhost:9090) 不可访问${NC}"
    echo "   如果 Prometheus 在其他端口/主机运行，请手动检查"
fi
echo ""

# 3. 检查关键指标查询
echo "📊 步骤 3: 通过 Prometheus API 查询指标"
if curl -s "http://localhost:9090/api/v1/query?query=indexer_blocks_processed_total" 2>/dev/null | grep -q '"result":\[{"metric"'; then
    echo -e "${GREEN}✅ Prometheus 中有 BlocksProcessed 数据${NC}"
else
    echo -e "${YELLOW}⚠️ Prometheus 中无 BlocksProcessed 数据${NC}"
    echo "   可能原因：抓取配置错误或索引器未运行"
fi
echo ""

# 4. 生成 Grafana Dashboard JSON 片段
echo "📊 步骤 4: 生成 Grafana Dashboard 配置"
cat > /tmp/grafana_panel.json << 'EOF'
{
  "dashboard": {
    "title": "Indexer Metrics Verification",
    "panels": [
      {
        "title": "Blocks Processed",
        "type": "stat",
        "targets": [{"expr": "indexer_blocks_processed_total", "legendFormat": "Blocks"}],
        "gridPos": {"h": 4, "w": 6, "x": 0, "y": 0}
      },
      {
        "title": "Transfers Processed",
        "type": "stat",
        "targets": [{"expr": "indexer_transfers_processed_total", "legendFormat": "Transfers"}],
        "gridPos": {"h": 4, "w": 6, "x": 6, "y": 0}
      },
      {
        "title": "Realtime TPS",
        "type": "graph",
        "targets": [{"expr": "indexer_realtime_tps", "legendFormat": "TPS"}],
        "gridPos": {"h": 8, "w": 12, "x": 0, "y": 4}
      }
    ]
  }
}
EOF
echo -e "${GREEN}✅ Grafana Panel 配置已生成: /tmp/grafana_panel.json${NC}"
echo ""

# 5. 总结
echo "📋 诊断总结:"
echo "   如果步骤 1 通过但步骤 2/3 失败，说明："
echo "   - 索引器指标导出正常 ✅"
echo "   - Prometheus 抓取配置有问题 ❌"
echo ""
echo "   建议操作："
echo "   1. 编辑 prometheus.yml，添加 job:"
echo '      scrape_configs:'
echo '        - job_name: "indexer-8082"'
echo '          static_configs:'
echo '            - targets: ["host.docker.internal:8082"]  # Docker 访问宿主机'
echo '          metrics_path: /metrics'
echo '          scrape_interval: 5s'
echo ""
echo "   2. 重启 Prometheus 服务"
echo ""
echo "   3. 在 Grafana 中添加 Prometheus 数据源:"
echo "      URL: http://prometheus:9090 (或实际地址)"
echo ""
