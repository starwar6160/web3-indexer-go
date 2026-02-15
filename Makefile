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
	@echo ""
	@echo "🔧 基础指令:"
	@echo "  make build        - 编译本地二进制文件"
	@echo "  make clean        - 清理构建产物"
	@echo "  make status       - 检查系统容器状态"

build:
	@echo "🛠️  Building shared indexer binary..."
	go build -o bin/$(BINARY_NAME) ./cmd/indexer

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
