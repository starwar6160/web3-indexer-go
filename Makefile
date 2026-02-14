# ==============================================================================
# Web3 Indexer 工业级控制台 (Commander)
# ==============================================================================

.PHONY: help build run air test test-quick test-cleanup clean demo start stop logs infra-up infra-down status stress-test docker-build sign-readme verify-identity deploy-service deploy-service-reset setup-demo

# 默认目标
help:
	@echo "可用指令:"
	@echo "  make demo         - [推荐] 一键启动 Docker 全栈演示环境 (含压测)"
	@echo "  make setup-demo   - 设置演示环境 (使用集中配置)"
	@echo "  make start        - 启动服务 (alias for demo)"
	@echo "  make stop         - 停止并清理 Docker 环境"
	@echo "  make status       - 检查容器运行状态"
	@echo "  make logs         - 查看实时索引日志"
	@echo "  make docker-build - 强制重新构建 Indexer 镜像"
	@echo "  make air          - [本地开发] 启动热重载 (需本地 Go 环境)"
	@echo "  make clean        - 清理本地构建产物"
	@echo "  make sign-readme  - 使用 EdDSA GPG 密钥签署 README.md"
	@echo "  make verify-identity - 验证存储库的加密身份"
	@echo "  make deploy-service - [生产] 编译并更新 systemd 服务运行新版本 (保留数据)"
	@echo "  make deploy-service-reset - [生产] 编译并更新 systemd 服务运行新版本 (清除数据)"
	@echo "  make test         - 运行所有测试（隔离环境，自动清理）"
	@echo "  make test-quick   - 快速运行测试（复用现有数据库，不清理）"

build:
	@echo "🔍 Running vet and build checks..."
	@go vet ./...
	@if [ $$? -ne 0 ]; then \
		echo "❌ Vet check failed"; \
		exit 1; \
	fi
	@echo "✅ Vet check passed"
	@go build -v ./cmd/...
	@if [ $$? -ne 0 ]; then \
		echo "❌ Build failed"; \
		exit 1; \
	fi
	@echo "✅ Build successful"
	@echo "📦 Creating release build..."
	./scripts/publish.sh

docker-build:
	docker compose build --no-cache

pulse:
	@curl -s -H "Content-Type: application/json" -X POST --data '{"jsonrpc":"2.0","method":"evm_setIntervalMining","params":[1],"id":1}' http://127.0.0.1:8545

run:
	go run ./cmd/indexer --reset

air:
	export PATH=$(PATH):$(shell go env GOPATH)/bin && air

infra-up:
	docker compose up -d db anvil

infra-down:
	docker compose down -v

demo:
	./setup/setup-demo.sh

start: demo

setup-demo:
	./setup/setup-demo.sh

stop:
	docker compose down -v
	@pkill air || true
	@pkill python3 || true

logs:
	docker compose logs -f indexer

sign-readme:
	gpg --detach-sign --armor --local-user F96525FE58575DCF README.md

verify-identity:
	@echo "验证 README 签名..."
	gpg --verify README.md.asc README.md
	@echo "\n验证公钥导出文件..."
	gpg --import PUBLIC_KEY.asc

deploy-service: build
	@echo "🚀 正在部署新版本到 systemd (保留现有数据)..."
	# 使用集中配置更新服务 (默认保留数据库数据)
	CLEAR_DB=false ./scripts/publish.sh
	sudo cp bin/web3-indexer.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl restart web3-indexer
	@echo "✅ 服务已重启，正在检查状态..."
	sudo systemctl status web3-indexer --no-pager

# Deploy service with database reset (optional)
deploy-service-reset: build
	@echo "🚀 正在部署新版本到 systemd (清除现有数据)..."
	# 使用集中配置更新服务 (清除数据库数据)
	CLEAR_DB=true ./scripts/publish.sh
	sudo cp bin/web3-indexer.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl restart web3-indexer
	@echo "✅ 服务已重启，正在检查状态..."
	sudo systemctl status web3-indexer --no-pager

# Run all tests (unit + integration) - isolated environment with auto cleanup
test:
	@echo "🧪 Starting isolated test environment..."
	@echo "📦 Project: web3_indexer_test"
	@echo "🔌 Port: 15433 (isolated from dev environment)"
	# 1. Start isolated test database with unique project name
	@docker compose -p web3_indexer_test -f docker-compose.test.yml up -d db
	# 2. Wait for database to be healthy
	@echo "⏳ Waiting for test database to be ready..."
	@until docker compose -p web3_indexer_test -f docker-compose.test.yml exec -T db pg_isready -U postgres > /dev/null 2>&1; do \
		sleep 1; \
	done
	@echo "✅ Test database ready"
	# 3. Run tests with isolated database
	@echo "🚀 Running tests in isolated environment..."
	@DATABASE_URL="postgres://postgres:postgres@localhost:15433/web3_indexer_test?sslmode=disable" \
		go test -v -count=1 ./internal/engine/... || (make test-cleanup && exit 1)
	# 4. Cleanup after success
	@make test-cleanup
	@echo "✅ All tests passed in isolated environment!"

# Quick test run - reuses existing database (for rapid iteration during development)
test-quick:
	@echo "🧪 Running all tests..."
	@echo "📦 Using existing database (no isolation)..."
	@docker compose up -d db || { echo "⚠️  Database already running or failed to start, continuing..."; }
	@echo "⏳ Waiting for database to be ready..."
	@sleep 3
	@echo "✅ Dependencies ready, running tests..."
	go test -v -count=1 ./internal/engine/...
	@echo "✅ All tests passed!"

# Cleanup isolated test environment
test-cleanup:
	@echo "🧹 Cleaning up isolated test environment..."
	@docker compose -p web3_indexer_test -f docker-compose.test.yml down -v --remove-orphans || true
	@echo "✅ Test environment cleaned up"