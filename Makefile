# ==============================================================================
# Web3 Indexer 工业级控制台 (Commander V3)
# ==============================================================================

# 全局变量
export BINARY_NAME := web3-indexer
export GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(GOPATH_BIN):$(PATH)

# 包含模块化配置
include makefiles/docker.mk
include makefiles/dev.mk
include makefiles/test.mk
include makefiles/docs.mk
include makefiles/db.mk

.PHONY: help build init clean status

# 默认目标
help:
	@echo "📦 部署与容器 (makefiles/docker.mk):"
	@echo "  make a1           - [调试] 启动 Sepolia 测试网容器 (8081)"
	@echo "  make a2           - [主力] 启动 Anvil 本地演示容器 (8080)"
	@echo "  make stop-all     - 停止并清理所有容器环境"
	@echo ""
	@echo "🚀 极速本地调试 (makefiles/dev.mk):"
	@echo "  make b1           - [Sepolia] 本地 go run 调试 (8081)"
	@echo "  make b2           - [Anvil]   本地 go run 调试 (8082)"
	@echo "  make stop-dev     - 清理本地调试占用的端口"
	@echo ""
	@echo "🧪 质量与文档 (makefiles/test.mk & docs.mk):"
	@echo "  make test-api     - 运行逻辑守卫集成测试 (Python)"
	@echo "  make check        - 运行所有质量检查 (Lint/Security/Test)"
	@echo "  make docs-sync    - 自动刷新文档索引 (SUMMARY.md)"
	@echo "  make repair       - [Sepolia] 异步修复数据库中的哈希链断裂 (0x000...)"
	@echo ""
	@echo "💾 数据库管理 (makefiles/db.mk):"
	@echo "  make db-list      - 查看所有 Web3 数据库统计"
	@echo "  make db-clean-debug     - 清空 Debug 数据库（保留结构）"
	@echo "  make db-reset-debug     - 重置 Debug 数据库（删除并重建）"
	@echo "  make db-clean-demo2     - 清空 Demo2 数据库（保留结构）"
	@echo "  make db-reset-demo2     - 重置 Demo2 数据库（删除并重建）"
	@echo "  make db-sync-schema     - 同步 Schema（Demo1 → Debug）"
	@echo "  make db-backup-demo1    - 备份 Demo1 数据"
	@echo "  make db-restore-demo1   - 恢复 Demo1 数据（从最新备份）"
	@echo ""
	@echo "🔧 基础指令:"
	@echo "  make build        - 编译本地二进制文件"
	@echo "  make clean        - 清理构建产物"
	@echo "  make status       - 检查系统容器状态"

build:
	@echo "🛠️  Building shared indexer binary (v1.0-Yokohama-Lab)..."
	go build -ldflags "-X main.Version=v1.0-Yokohama-Lab" -o bin/$(BINARY_NAME) ./cmd/indexer

clean:
	@echo "🧹 Cleaning up artifacts..."
	rm -rf bin/ tmp/ *.log .air_*.log .air_*.pid
	@echo "✅ Clean complete."

status:
	@echo "📊 Container Status:"
	@docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" | grep web3 || echo "No active indexer containers."

# 首次运行初始化
init:
	@echo "🚀 Initializing environment..."
	@mkdir -p bin logs tmp
	@if [ ! -f .env.testnet.local ]; then cp .env.testnet .env.testnet.local; fi
	@if [ ! -f .env.demo2 ]; then cp .env.example .env.demo2; fi
	@echo "✅ Environment ready."

# 辅助指令：Sepolia 预检
a1-pre-flight:
	@echo "🔍 Running Sepolia pre-flight checks..."
	@./scripts/check-a1-pre-flight.sh

# --- 网关管理指令 ---
gateway-config:
	@chmod +x scripts/gen-nginx-config.sh
	@./scripts/gen-nginx-config.sh

gateway-reload: gateway-config
	@echo "♻️  Reloading Nginx Gateway..."
	@docker exec web3-indexer-gateway nginx -s reload
	@echo "✅ Gateway config updated and reloaded."

