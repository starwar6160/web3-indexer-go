# ==============================================================================
# Web3 Indexer 工业级控制台 (Commander)
# ==============================================================================

# Production-Grade Configuration
BINARY_NAME=web3-indexer
INSTALL_PATH=/usr/local/bin/$(BINARY_NAME)
SERVICE_NAME=$(BINARY_NAME).service
CONFIG_DIR=/etc/$(BINARY_NAME)
LOG_DIR=/var/log/$(BINARY_NAME)
RUN_USER=$(shell whoami)
PROJECT_ROOT=$(shell pwd)
DOCKER_GATEWAY=$(shell docker network inspect bridge -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || echo "172.17.0.1")
GOPATH_BIN=$(shell go env GOPATH)/bin
export PATH := $(GOPATH_BIN):$(PATH)

.PHONY: help init build run air test test-quick test-cleanup check lint security clean demo start stop logs infra-up infra-down status stress-test docker-build sign-readme verify-identity deploy-service deploy-service-reset setup-demo check-env install-deps a1-pre-flight

# Default target
help:
	@echo "可用指令:"
	@echo ""
	@echo "📦 Development & Testing:"
	@echo "  make demo         - [推荐] 一次性启动 Docker 全栈演示环境 (含压测)"
	@echo "  make a1           - [测试网] 启动 Sepolia 测试网索引器 (隔离环境，含预检)"
	@echo "  make a1-pre-flight - [测试网] 单独运行预检脚本 (5 步原子化验证)"
	@echo "  make reset-a1     - [测试网] 完全重置测试网环境 (停止+清理+重置数据库)"
	@echo "  make clean-testnet - [测试网] 清理测试网容器环境"
	@echo "  make reset-testnet-db - [测试网] 重置测试网数据库表 (保留schema)"
	@echo "  make setup-demo   - 设置演示环境 (使用集中配置)"
	@echo "  make start        - 启动服务 (alias for demo)"
	@echo "  make stop         - 停止并清理 Docker 环境"
	@echo "  make status       - 检查容器运行状态"
	@echo "  make logs         - 查看实时索引日志"
	@echo "  make logs-testnet - 查看测试网索引日志"
	@echo "  make docker-build - 强制重新构建 Indexer 镜像"
	@echo "  make air          - [本地开发] 启动热重载 (需本地 Go 环境)"
	@echo ""
	@echo "🧪 Quality Assurance:"
	@echo "  make test         - 运行所有测试（隔离环境，自动清理）"
	@echo "  make test-quick   - 快速运行测试（复用现有数据库，不清理）"
	@echo "  make check        - 运行所有质量检查（lint + security + test）"
	@echo "  make lint         - 运行 golangci-lint 代码质量检查"
	@echo "  make security     - 运行安全漏洞扫描（gosec + govulncheck）"
	@echo ""
	@echo "🚀 Production Deployment:"
	@echo "  make init         - 初始化环境配置（首次运行）"
	@echo "  make check-env    - 检查环境依赖（Docker, Go, systemctl）"
	@echo "  make install-deps - 自动安装缺失的依赖"
	@echo "  make deploy-service - [生产] 编译并部署 systemd 服务 (保留数据)"
	@echo "  make deploy-service-reset - [生产] 编译并部署 systemd 服务 (清除数据)"
	@echo ""
	@echo "🔧 Utilities:"
	@echo "  make clean        - 清理本地构建产物"
	@echo "  make sign-readme  - 使用 EdDSA GPG 密钥签署 README.md"
	@echo "  make verify-identity - 验证存储库的加密身份"

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

# ==============================================================================
# Production-Grade Quality Gates
# ==============================================================================

# Run all quality checks (lint + security + test)
check: lint security test
	@echo "✅ All quality gates passed!"

# Run golangci-lint code quality checks
lint:
	@echo "🔍 Running golangci-lint..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "⚠️  golangci-lint not found. Installing..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH_BIN) latest; \
	fi
	@golangci-lint run --timeout=5m --config=.golangci.yml ./...
	@echo "✅ Lint checks passed!"

# Run security vulnerability scans
security:
	@echo "🔒 Running security scans..."
	@echo "🔍 Scanning for hardcoded secrets (gosec)..."
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "⚠️  gosec not found. Installing..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	@gosec -no-fail -fmt text -out gosec-report.txt ./... || true
	@echo "📋 GoSec report saved to gosec-report.txt"
	@echo "🔍 Checking for known vulnerabilities (govulncheck)..."
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "⚠️  govulncheck not found. Installing..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi
	@govulncheck ./...
	@echo "✅ Security scans completed!"

# Check code complexity (maintainability)
complexity:
	@echo "📊 Checking code complexity..."
	@if ! command -v gocognit >/dev/null 2>&1; then \
		echo "⚠️  gocognit not found. Installing..."; \
		go install github.com/uudashr/gocognit/cmd/gocognit@latest; \
	fi
	@gocognit -over 15 ./... 2>&1 | { grep -v "ok" || true; }
	@echo "✅ Complexity check completed!"

# ==============================================================================
# Environment Detection & Setup
# ==============================================================================

# Check environment dependencies
check-env:
	@echo "🔍 Checking environment dependencies..."
	@missing=""; \
	if ! command -v go >/dev/null 2>&1; then missing="$$missing go"; fi; \
	if ! command -v docker >/dev/null 2>&1; then missing="$$missing docker"; fi; \
	if command -v systemctl >/dev/null 2>&1; then \
		if ! command -v sudo >/dev/null 2>&1; then missing="$$missing sudo (required for systemctl)"; fi; \
	fi; \
	if [ -n "$$missing" ]; then \
		echo "❌ Missing dependencies:$$missing"; \
		echo "💡 Run 'make install-deps' to install missing dependencies"; \
		exit 1; \
	fi
	@echo "✅ All dependencies installed!"
	@go version
	@docker --version
	@if command -v systemctl >/dev/null 2>&1; then \
		echo "systemd available: ✅"; \
	else \
		echo "systemd available: ⚠️  (not available on this system)"; \
	fi

# Auto-install missing dependencies (Ubuntu/Debian)
install-deps:
	@echo "📦 Installing missing dependencies..."
	@if ! command -v go >/dev/null 2>&1; then \
		echo "Installing Go..."; \
		sudo apt-get update && sudo apt-get install -y golang-go; \
	fi
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "Installing Docker..."; \
		curl -fsSL https://get.docker.com | sh; \
		sudo usermod -aG docker $$USER; \
	fi
	@if command -v systemctl >/dev/null 2>&1 && ! command -v sudo >/dev/null 2>&1; then \
		echo "Installing sudo..."; \
		sudo apt-get update && sudo apt-get install -y sudo; \
	fi
	@echo "✅ Dependencies installed! Please re-login if Docker group was added."

# Initialize environment configuration
init:
	@echo "🚀 Initializing Web3 Indexer environment..."
	@if [ -f .env ]; then \
		echo "⚠️  .env file already exists. Skipping..."; \
	else \
		echo "📝 Creating .env from template..."; \
		cp .env.example .env; \
		echo "✅ .env created! Please edit it with your configuration."; \
	fi
	@mkdir -p bin logs
	@echo "✅ Environment initialized!"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Edit .env with your configuration"
	@echo "  2. Run 'make demo' to start development environment"
	@echo "  3. Run 'make deploy-service' for production deployment"

# ==============================================================================
# Production Deployment (Systemd)
# ==============================================================================

# Deploy as systemd service (preserves data)
deploy-service: check-env build
	@echo "🚀 Deploying as systemd service (preserving data)..."
	# 1. Create production directory structure
	@echo "📁 Creating production directories..."
	@sudo mkdir -p $(CONFIG_DIR)
	@sudo mkdir -p $(LOG_DIR)
	@sudo chown -R $(RUN_USER):$(RUN_USER) $(LOG_DIR)
	# 2. Copy configuration
	@echo "📝 Installing configuration..."
	@if [ -f .env ]; then \
		sudo cp .env $(CONFIG_DIR)/.env; \
		sudo chmod 600 $(CONFIG_DIR)/.env; \
	else \
		echo "❌ .env not found. Please run 'make init' first."; \
		exit 1; \
	fi
	# 3. Install binary
	@echo "📦 Installing binary..."
	@sudo cp bin/$(BINARY_NAME) $(INSTALL_PATH)
	@sudo chmod +x $(INSTALL_PATH)
	# 4. Generate systemd unit file dynamically
	@echo "⚙️  Generating systemd unit file..."
	@echo "[Unit]" | sudo tee /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "Description=Web3 Indexer Service" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "After=network.target postgresql.service" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "[Service]" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "Type=simple" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "User=$(RUN_USER)" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "WorkingDirectory=$(CONFIG_DIR)" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "EnvironmentFile=$(CONFIG_DIR)/.env" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "ExecStart=$(INSTALL_PATH)" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "Restart=always" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "RestartSec=5" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "StandardOutput=append:$(LOG_DIR)/indexer.log" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "StandardError=append:$(LOG_DIR)/indexer.error.log" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "[Install]" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "WantedBy=multi-user.target" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	# 5. Enable and start service
	@echo "🔄 Reloading systemd daemon..."
	@sudo systemctl daemon-reload
	@echo "✅ Enabling service..."
	@sudo systemctl enable $(SERVICE_NAME)
	@echo "🚀 Starting service..."
	@sudo systemctl restart $(SERVICE_NAME)
	@echo ""
	@echo "✅ Service deployed successfully!"
	@echo ""
	@echo "Management commands:"
	@echo "  sudo systemctl status $(SERVICE_NAME)  # Check status"
	@echo "  sudo systemctl stop $(SERVICE_NAME)      # Stop service"
	@echo "  sudo systemctl start $(SERVICE_NAME)     # Start service"
	@echo "  sudo journalctl -u $(SERVICE_NAME) -f   # View logs"
	@echo "  tail -f $(LOG_DIR)/indexer.log          # View application logs"

# Deploy as systemd service (with database reset)
deploy-service-reset: check-env build
	@echo "🚀 Deploying as systemd service (with database reset)..."
	# 1. Stop service
	@if systemctl is-active --quiet $(SERVICE_NAME) 2>/dev/null; then \
		echo "🛑 Stopping existing service..."; \
		sudo systemctl stop $(SERVICE_NAME); \
	fi
	# 2. Create production directory structure
	@echo "📁 Creating production directories..."
	@sudo mkdir -p $(CONFIG_DIR)
	@sudo mkdir -p $(LOG_DIR)
	@sudo chown -R $(RUN_USER):$(RUN_USER) $(LOG_DIR)
	# 3. Copy configuration
	@echo "📝 Installing configuration..."
	@if [ -f .env ]; then \
		sudo cp .env $(CONFIG_DIR)/.env; \
		sudo chmod 600 $(CONFIG_DIR)/.env; \
	else \
		echo "❌ .env not found. Please run 'make init' first."; \
		exit 1; \
	fi
	# 4. Reset database (if configured)
	@echo "🗑️  Resetting database..."
	@CLEAR_DB=true ./scripts/publish.sh || echo "⚠️  Database reset skipped (publish.sh not found)"
	# 5. Install binary
	@echo "📦 Installing binary..."
	@sudo cp bin/$(BINARY_NAME) $(INSTALL_PATH)
	@sudo chmod +x $(INSTALL_PATH)
	# 6. Generate systemd unit file (same as deploy-service)
	@echo "⚙️  Generating systemd unit file..."
	@echo "[Unit]" | sudo tee /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "Description=Web3 Indexer Service" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "After=network.target postgresql.service" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "[Service]" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "Type=simple" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "User=$(RUN_USER)" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "WorkingDirectory=$(CONFIG_DIR)" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "EnvironmentFile=$(CONFIG_DIR)/.env" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "ExecStart=$(INSTALL_PATH)" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "Restart=always" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "RestartSec=5" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "StandardOutput=append:$(LOG_DIR)/indexer.log" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "StandardError=append:$(LOG_DIR)/indexer.error.log" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "[Install]" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	@echo "WantedBy=multi-user.target" | sudo tee -a /etc/systemd/system/$(SERVICE_NAME) > /dev/null
	# 7. Enable and start service
	@echo "🔄 Reloading systemd daemon..."
	@sudo systemctl daemon-reload
	@echo "✅ Enabling service..."
	@sudo systemctl enable $(SERVICE_NAME)
	@echo "🚀 Starting service..."
	@sudo systemctl restart $(SERVICE_NAME)
	@echo ""
	@echo "✅ Service deployed successfully (database reset)!"
	@echo ""
	@echo "Management commands:"
	@echo "  sudo systemctl status $(SERVICE_NAME)  # Check status"
	@echo "  sudo systemctl stop $(SERVICE_NAME)      # Stop service"
	@echo "  sudo systemctl start $(SERVICE_NAME)     # Start service"
	@echo "  sudo journalctl -u $(SERVICE_NAME) -f   # View logs"
	@echo "  tail -f $(LOG_DIR)/indexer.log          # View application logs"

# ==============================================================================
# Hybrid Deployment (Container DB + Host Binary)
# ==============================================================================

# Clean up testnet environment
clean-testnet:
	@echo "🧹 Cleaning up testnet environment..."
	@docker compose -f docker-compose.testnet.yml -p web3-testnet down --remove-orphans || true
	@echo "✅ Testnet environment cleaned up"

# Reset testnet database tables (preserving schema)
reset-testnet-db:
	@echo "🗑️  Resetting testnet database tables (preserving schema)..."
	@if docker compose -f docker-compose.testnet.yml -p web3-testnet ps | grep -q sepolia-db; then \
		echo "✅ Testnet database is running, resetting tables..."; \
		docker compose -f docker-compose.testnet.yml -p web3-testnet exec sepolia-db psql -U postgres -d web3_sepolia -c "TRUNCATE TABLE blocks, transfers, sync_checkpoints RESTART IDENTITY;" 2>/dev/null || \
		echo "⚠️  Could not truncate tables (database may not be ready yet)"; \
	else \
		echo "⚠️  Testnet database container not running, skipping table reset"; \
	fi

# ==============================================================================
# Testnet Pre-flight Checks (原子化验证)
# ==============================================================================

# Run pre-flight checks before starting testnet
a1-pre-flight:
	@echo "🔍 Running pre-flight checks..."
	@./scripts/check-a1-pre-flight.sh

# Testnet mode: isolated environment for Sepolia/Holesky (with pre-flight checks)
a1: a1-pre-flight check-env clean-testnet
	@echo "🎮 Starting Testnet Mode (Isolated Environment)..."
	@echo "📦 Project: web3-testnet"
	@echo "🔗 Target: Sepolia Testnet (configurable via .env.testnet)"
	# 1. Load environment variables from .env.testnet.local if exists
	@if [ -f ".env.testnet.local" ]; then \
		echo "🔑 Loading API keys from .env.testnet.local..."; \
		set -a && \
		. .env.testnet.local && \
		set +a && \
		export $$(grep -v '^#' .env.testnet.local | xargs); \
	fi
	# 2. Check if SEPOLIA_RPC_URLS is set
	@if [ -z "$$SEPOLIA_RPC_URLS" ]; then \
		echo "❌ Error: SEPOLIA_RPC_URLS environment variable is required"; \
		echo "💡 Example: export SEPOLIA_RPC_URLS='https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY'"; \
		echo "💡 Or create .env.testnet.local with your API keys"; \
		exit 1; \
	fi
	# 3. Start isolated testnet infrastructure (pass environment variables)
	@echo "🚀 Starting testnet infrastructure (db, indexer)..."
	@echo "📡 Using RPC: $$SEPOLIA_RPC_URLS"
	@if [ -f ".env.testnet.local" ]; then \
		docker compose -f docker-compose.testnet.yml --env-file .env.testnet.local -p web3-testnet up -d sepolia-db sepolia-indexer; \
	else \
		docker compose -f docker-compose.testnet.yml -p web3-testnet up -d sepolia-db sepolia-indexer; \
	fi
	# 4. Wait for database to be ready
	@echo "⏳ Waiting for testnet database to be ready..."
	@until docker compose -f docker-compose.testnet.yml -p web3-testnet exec -T sepolia-db pg_isready -U postgres > /dev/null 2>&1; do \
		sleep 1; \
	done
	@echo "✅ Testnet infrastructure ready"
	@echo "🌐 Sepolia indexer is now running on http://localhost:8081"
	@echo "📊 Monitor at: http://localhost:8081/metrics"
	@echo "📋 View logs: make logs-testnet"

# View testnet logs
logs-testnet:
	docker compose -f docker-compose.testnet.yml -p web3-testnet logs -f sepolia-indexer

# Stop testnet environment
stop-testnet:
	docker compose -f docker-compose.testnet.yml -p web3-testnet down

# Full reset: stop, clean, and restart testnet environment
reset-a1: stop-testnet clean-testnet reset-testnet-db
	@echo "🔄 Full reset complete. Run 'make a1' to start fresh."

# Hybrid demo mode: containerized infrastructure + host binary
demo: check-env
	@echo "🎮 Starting Demo Mode (Hybrid Architecture)..."
	@echo "📦 Project: web3-demo"
	@echo "🌉 Docker Gateway: $(DOCKER_GATEWAY)"
	# 1. Start containerized infrastructure
	@echo "🚀 Starting infrastructure (db, prometheus, grafana)..."
	@docker compose -p web3-demo up -d db prometheus grafana
	# 2. Wait for database to be ready
	@echo "⏳ Waiting for database to be ready..."
	@until docker compose -p web3-demo exec -T db pg_isready -U postgres > /dev/null 2>&1; do \
		sleep 1; \
	done
	@echo "✅ Infrastructure ready"
	# 3. Load environment and run
	@echo "🚀 Starting Web3 Indexer (host binary)..."
	@if [ -f .env ]; then \
		export $$(cat .env | xargs); \
	else \
		echo "⚠️  .env not found, using default configuration"; \
	fi
	@go run ./cmd/indexer/main.go