#!/bin/bash
# ==============================================================================
# Grafana 多数据源自动配置脚本
# ==============================================================================

GRAFANA_URL="http://localhost:4000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="W3b3_Idx_Secur3_2026_Sec"

echo "🔧 配置 Grafana 多数据源..."

# 检查并创建 Demo1 数据源
echo ""
echo "📊 配置 Demo1 数据源..."
RESPONSE=$(curl -s -X POST "$GRAFANA_URL/api/datasources" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PostgreSQL-Demo1",
    "type": "grafana-postgresql-datasource",
    "url": "localhost:15432",
    "user": "postgres",
    "password": "W3b3_Idx_Secur3_2026_Sec",
    "database": "web3_indexer_demo1",
    "sslMode": "disable",
    "isDefault": false,
    "uid": "postgres_demo1_ds"
  }')

echo "$RESPONSE" | jq -r '.message // ."datasource.id" // "已存在或创建失败"' 2>/dev/null || echo "$RESPONSE"

# 检查并创建 Debug 数据源
echo ""
echo "🐛 配置 Debug 数据源..."
RESPONSE=$(curl -s -X POST "$GRAFANA_URL/api/datasources" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PostgreSQL-Debug",
    "type": "grafana-postgresql-datasource",
    "url": "localhost:15432",
    "user": "postgres",
    "password": "W3b3_Idx_Secur3_2026_Sec",
    "database": "web3_indexer_debug",
    "sslMode": "disable",
    "isDefault": false,
    "uid": "postgres_debug_ds"
  }')

echo "$RESPONSE" | jq -r '.message // ."datasource.id" // "已存在或创建失败"' 2>/dev/null || echo "$RESPONSE"

echo ""
echo "✅ Grafana 数据源配置完成！"
echo ""
echo "📋 可用的数据源："
echo "  - PostgreSQL (原始)"
echo "  - PostgreSQL-Demo1"
echo "  - PostgreSQL-Debug"
echo ""
echo "🔍 验证数据源："
echo "  访问 $GRAFANA_URL -> Configuration -> Data sources"
echo ""
echo "💡 提示：在 Dashboard 面板中选择正确的数据源以查看不同环境的数据"
