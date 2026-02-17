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
	@./scripts/infra/check-a1-pre-flight.sh

# --- 生产级环境清理与重启 ---
.PHONY: reset-8091-live
reset-8091-live: stop-dev build
	@echo "🚨 [ENVIRONMENT RESET] Cleaning Sepolia (8091) environment..."
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO1_DB) \
	  -c "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;"
	@echo "✅ Database cleaned. Starting fresh Sepolia indexer with SECURITY LOCK..."
	@NETWORK_MODE=sepolia ENABLE_SIMULATOR=false CHAIN_ID=11155111 PORT=8081 \
	  ./bin/$(BINARY_NAME) --start-from latest &
	@echo "🚀 Sepolia indexer is running in background (Port 8081). Check logs/ for progress."

# --- 网关管理指令 ---
gateway-config:
	@chmod +x scripts/gen-nginx-config.sh
	@./scripts/gen-nginx-config.sh

gateway-reload: gateway-config
	@echo "♻️  Reloading Nginx Gateway..."
	@docker exec web3-indexer-gateway nginx -s reload
	@echo "✅ Gateway config updated and reloaded."

.PHONY: ci
ci:
	@echo "🚀 开始本地 CI 仿真验证..."
	docker build -f Dockerfile.ci -t web3-indexer-ci:local .
	docker run --rm -u $$(id -u):$$(id -g) \
		-e GOCACHE=/tmp/go-cache \
		-e GOMODCACHE=/tmp/go-mod-cache \
		-e GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache \
		-e TRIVY_CACHE_DIR=/tmp/trivy-cache \
		-v $(PWD):/app \
		web3-indexer-ci:local

# Anvil 快捷命令
.PHONY: anvil-status anvil-reset anvil-inject anvil-inject-defi anvil-verify anvil-pro
anvil-status:
	@echo "📊 Anvil 状态检查..."
	@echo "当前高度: $$(shell scripts/get-anvil-height.sh)"
	@curl -s http://127.0.0.1:8545 -X POST \
	  -H "Content-Type: application/json" \
	  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}' \
	  | jq '{number: .result.number, hash: .result.hash, transactions: .result.transactions | length}'

anvil-reset:
	@echo "🚨 重置 Anvil 和 Demo2 数据库..."
	@PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
	  -c "TRUNCATE TABLE blocks, transfers CASCADE; DELETE FROM sync_checkpoints;"
	@echo "✅ 数据库已清空，下次启动将从创世块开始"

anvil-inject:
	@echo "💉 注入基础 Synthetic Transfers..."
	@PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
	  -c "INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address) VALUES \
	  (60390, '0xabcd0001', 99999, '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266', '0x70997970c51812dc3a010c7d01b50e0d17dc79ee', '1000000000000000000', '0x0000000000000000000000000000000000000000'), \
	  (60389, '0xabcd0002', 99999, '0x70997970c51812dc3a010c7d01b50e0d17dc79ee', '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc', '2000000000000000000', '0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48'), \
	  (60388, '0xabcd0003', 99999, '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc', '0x90f79bf6eb2c4f870365e785982e1f101e93b906', '3000000000000000000', '0xdac17f958d2ee523a2206206994597c13d831ec7') \
	  ON CONFLICT (block_number, log_index) DO NOTHING;"
	@echo "✅ 已注入 3 笔基础 Synthetic Transfers"
	@PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
	  -t -c "SELECT COUNT(*) as total FROM transfers;"
	@echo "💡 访问 http://localhost:8092 查看效果"

anvil-inject-defi:
	@echo "🏭 注入 DeFi 高频交易（套利/Flashloan/MEV）..."
	@PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
	  -f scripts/inject-defi-transfers.sql
	@echo ""
	@echo "✅ DeFi 模拟数据已注入！"
	@echo ""
	@echo "📊 交易类型分布："
	@echo "   🔄 Swap: 60% (普通交易)"
	@echo "   🦈 Arbitrage: 20% (套利，大额)"
	@echo "   ⚡ Flashloan: 10% (闪电贷，巨额)"
	@echo "   🦈 MEV: 10% (夹子攻击)"
	@echo ""
	@PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo \
	  -t -c "SELECT COUNT(*) as total FROM transfers WHERE block_number >= 60400;"
	@echo "💡 刷新 http://localhost:8092 查看效果"

anvil-verify:
	@bash scripts/verify-web-ui.sh

anvil-pro:
	@echo "🏭 启动 Anvil Pro 实验室..."
	@bash scripts/start-anvil-pro-lab.sh
