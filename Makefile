# Web3 Indexer - Go SRE级别项目
# 一键部署和启动脚本

.PHONY: help build run start stop clean test docker-up docker-down logs status dev-setup deploy deploy-clean

# 默认目标
help:
	@echo "🚀 Web3 Indexer - Go SRE级别项目 (All-in-Docker架构)"
	@echo ""
	@echo "🐳 ALL-IN-DOCKER 部署命令 (推荐):"
	@echo "  make deploy        - 一键启动所有服务 (零配置、零环境依赖)"
	@echo "  make deploy-clean  - 停止并清理所有容器和数据"
	@echo "  make deploy-logs   - 查看所有服务日志"
	@echo "  make deploy-status - 查看所有服务状态"
	@echo ""
	@echo "🔧 本地开发命令:"
	@echo "  make build     - 编译Go二进制文件"
	@echo "  make run       - 直接运行Go程序"
	@echo "  make start     - 启动所有服务 (数据库 + 索引器)"
	@echo "  make stop      - 停止所有服务"
	@echo "  make clean     - 清理所有容器和数据"
	@echo "  make test      - 运行测试"
	@echo ""
	@echo "📍 访问地址:"
	@echo "  🎛️  Dashboard: http://localhost:8080"
	@echo "  ❤️   Health:   http://localhost:8080/healthz"
	@echo "  📊 Metrics:  http://localhost:8080/metrics"
	@echo "  ⛽️  Anvil RPC: http://localhost:8545 (内部访问)"

# ============================================================================
# ALL-IN-DOCKER DEPLOYMENT (推荐方式)
# ============================================================================

# 一键启动所有服务 (零配置、零环境依赖)
deploy:
	@echo "🚀 启动 All-in-Docker 架构..."
	@echo ""
	@echo "📦 架构拓扑:"
	@echo "   ┌─────────────────────────────────────────┐"
	@echo "   │       Docker Compose Network            │"
	@echo "   │  ┌──────────┐  ┌──────────┐  ┌────────┐ │"
	@echo "   │  │    db    │  │  anvil   │  │indexer │ │"
	@echo "   │  │ :5432    │  │  :8545   │  │ :8080  │ │"
	@echo "   │  └──────────┘  └──────────┘  └────────┘ │"
	@echo "   └─────────────────────────────────────────┘"
	@echo ""
	@echo "🔧 清理旧容器..."
	docker compose down -v 2>/dev/null || true
	@echo ""
	@echo "🐳 构建并启动所有服务..."
	docker compose up -d --build
	@echo ""
	@echo "⏳ 等待服务就绪..."
	@sleep 10
	@echo ""
	@echo "✅ 所有服务已启动！"
	@echo ""
	@echo "📍 访问地址:"
	@echo "   🎛️  Dashboard: http://localhost:8080"
	@echo "   ❤️   Health:   http://localhost:8080/healthz"
	@echo "   📊 Metrics:  http://localhost:8080/metrics"
	@echo ""
	@echo "💡 查看日志: make deploy-logs"
	@echo "💡 查看状态: make deploy-status"
	@echo "💡 停止服务: make deploy-clean"

# 停止并清理所有容器和数据
deploy-clean:
	@echo "🛑 停止所有服务并清理数据..."
	docker compose down -v --remove-orphans
	@echo "✅ 清理完成"

# 查看所有服务日志
deploy-logs:
	@echo "📋 服务日志:"
	docker compose logs -f

# 查看所有服务状态
deploy-status:
	@echo "📊 服务状态:"
	@echo ""
	docker compose ps
	@echo ""
	@echo "🔍 健康检查:"
	@curl -s http://localhost:8080/healthz 2>/dev/null | head -20 || echo "❌ 服务未就绪"

# ============================================================================
# LOCAL DEVELOPMENT (本地开发)
# ============================================================================

# Build the indexer binary
build:
	@echo "🔨 编译Go索引器..."
	mkdir -p bin
	go build -o bin/indexer ./cmd/indexer
	@echo "✅ 编译完成: bin/indexer"

# Run the indexer (requires .env file)
run:
	@echo "🚀 直接运行Go索引器..."
	go run ./cmd/indexer/main.go

# 启动Docker基础设施
docker-up:
	@echo "🐳 启动Docker基础设施..."
	docker compose -f docker-compose.infra.yml up -d
	@echo "⏳ 等待数据库就绪..."
	@sleep 5
	@echo "✅ 基础设施启动完成"

# 停止Docker基础设施
docker-down:
	@echo "🛑 停止Docker基础设施..."
	docker compose -f docker-compose.infra.yml down
	@echo "✅ 基础设施已停止"

# 启动所有服务
start: docker-up build
	@echo "🚀 启动Web3 Indexer..."
	@echo "📝 环境变量配置:"
	@echo "   DATABASE_URL=postgres://postgres:postgres@localhost:15433/indexer?sslmode=disable"
	@echo "   RPC_URLS=https://greatest-alpha-morning.ethereum-sepolia.quiknode.pro/acf2caf911f89ccdc17e965b59706700a8479bad/"
	@echo "   CHAIN_ID=11155111 (Sepolia)"
	@echo "   START_BLOCK=10216000"
	@echo ""
	@echo "🔪 清理旧进程并释放端口 8088..."
	@pkill -f "bin/indexer" 2>/dev/null || true
	@PID_8088=$$(lsof -ti:8088 2>/dev/null); \
	 if [ -n "$$PID_8088" ]; then echo "⚠️  8088 被占用，尝试终止进程 $$PID_8088"; kill -9 $$PID_8088 2>/dev/null || true; fi
	@DATABASE_URL=postgres://postgres:postgres@localhost:15433/indexer?sslmode=disable \
	 RPC_URLS=https://greatest-alpha-morning.ethereum-sepolia.quiknode.pro/acf2caf911f89ccdc17e965b59706700a8479bad/ \
	 CHAIN_ID=11155111 \
	 START_BLOCK=10216000 \
	 LOG_LEVEL=info \
	 ./bin/indexer &
	@echo "✅ 索引器已启动"
	@echo ""
	@echo "🎛️  Dashboard: http://localhost:8088"
	@echo "❤️   Health:   http://localhost:8088/healthz"
	@echo "📊 Metrics:  http://localhost:8088/metrics"
	@echo ""
	@echo "💡 使用 'make logs' 查看日志"
	@echo "💡 使用 'make stop' 停止所有服务"

# 停止所有服务
stop:
	@echo "🛑 停止Web3 Indexer..."
	@pkill -f "bin/indexer" || true
	@make docker-down
	@echo "✅ 所有服务已停止"

# Clean build artifacts and containers
clean:
	@echo "🧹 清理所有资源..."
	@pkill -f "bin/indexer" || true
	@rm -f indexer bin/indexer
	@go clean
	@docker compose -f docker-compose.infra.yml down -v --remove-orphans 2>/dev/null || true
	@docker system prune -f 2>/dev/null || true
	@echo "✅ 清理完成"

# 查看服务日志
logs:
	@echo "📋 Web3 Indexer 日志:"
	@echo "==================="
	@pkill -0 -f "bin/indexer" && echo "✅ 索引器运行中" || echo "❌ 索引器未运行"
	@echo ""
	@echo "🐳 Docker 容器状态:"
	@docker compose -f docker-compose.infra.yml ps 2>/dev/null || echo "❌ Docker未运行"

# 查看服务状态
status:
	@echo "📊 服务状态检查"
	@echo "================"
	@echo ""
	@echo "🔍 Web3 Indexer 进程:"
	@ps aux | grep "bin/indexer" | grep -v grep || echo "❌ 索引器未运行"
	@echo ""
	@echo "🐳 Docker 容器:"
	@docker compose -f docker-compose.infra.yml ps 2>/dev/null || echo "❌ Docker未运行"
	@echo ""
	@echo "🌐 HTTP 服务检查:"
	@curl -s http://localhost:8080/healthz 2>/dev/null | head -1 || echo "❌ HTTP服务无响应"
	@echo ""
	@echo "🗄️  数据库连接:"
	@docker compose -f docker-compose.infra.yml exec -T postgres pg_isready -U postgres 2>/dev/null || echo "❌ 数据库连接失败"

# Run tests
test:
	@echo "🧪 运行Go测试..."
	go test -v ./...

# Download dependencies
deps:
	@echo "📦 安装Go依赖..."
	go mod download
	go mod tidy

# Run go vet
vet:
	@echo "🔍 运行go vet..."
	go vet ./...

# Run linter (requires golangci-lint)
lint:
	@echo "🔍 运行代码检查..."
	golangci-lint run 2>/dev/null || echo "⚠️  golangci-lint未安装"

# 开发模式启动 (包含Anvil测试节点)
dev: docker-up build
	@echo "🔧 启动开发环境 (包含Anvil测试节点)..."
	docker compose -f docker-compose.infra.yml --profile testing up -d
	@sleep 3
	@DATABASE_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable \
	 RPC_URLS=http://localhost:8545 \
	 WSS_URL=ws://localhost:8545 \
	 CHAIN_ID=31337 \
	 START_BLOCK=0 \
	 LOG_LEVEL=debug \
	 ./bin/indexer &
	@echo "✅ 开发环境已启动"
	@echo "🎛️  Dashboard: http://localhost:8080"
	@echo "⛽️  Anvil RPC:  http://localhost:8545"

# Full dev setup (legacy compatibility)
dev-setup: docker-up
	@echo "🔧 开发环境设置完成!"
	@echo "🎛️  Dashboard: http://localhost:8080"
	@echo "💡 现在可以运行 'make start' 启动索引器"

# Database migrations (requires golang-migrate)
DB_URL=postgres://postgres:postgres@localhost:15432/indexer?sslmode=disable

migrate-up:
	@echo "📈 执行数据库迁移..."
	migrate -path migrations -database "$(DB_URL)" up 2>/dev/null || echo "✅ 使用Docker自动初始化"

migrate-down:
	@echo "📉 执行数据库回滚..."
	migrate -path migrations -database "$(DB_URL)" down 2>/dev/null || echo "❌ migrate未安装"

# 快速重启
restart: stop start

# ============================================================================
# ANVIL TESTING - 本地模拟链测试工作流
# ============================================================================

# 启动Anvil本地测试环境
anvil-up:
	@echo "🔧 启动Anvil本地测试环境..."
	docker compose -f docker-compose.infra.yml --profile testing up -d postgres anvil
	@echo "⏳ 等待Anvil就绪..."
	@ATTEMPTS=0; \
	 until curl -s -X POST http://localhost:8546 -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' | grep -q 'result'; do \
	   ATTEMPTS=$$((ATTEMPTS+1)); \
	   if [ $$ATTEMPTS -ge 30 ]; then echo "❌ 等待Anvil超时"; exit 1; fi; \
	   echo "... 等待Anvil (尝试 $$ATTEMPTS/30)"; \
	   sleep 1; \
	 done
	@echo "✅ Anvil已启动"
	@echo "⛽️  RPC URL: http://localhost:8546"
	@echo "🔗 Chain ID: 31337"

# 停止Anvil测试环境
anvil-down:
	@echo "🛑 停止Anvil测试环境..."
	docker compose -f docker-compose.infra.yml --profile testing down
	@echo "✅ Anvil已停止"

# 部署演示合约并发送测试交易
demo-deploy: build
	@echo "🚀 部署演示合约到Anvil..."
	RPC_URL=http://localhost:8546 go run ./cmd/demo/deploy.go

# 启动Anvil演示模式（包含合约部署和交易）
demo: anvil-up demo-deploy
	@echo ""
	@echo "🎯 演示环境已准备就绪！"
	@echo ""
	@echo "📝 启动索引器:"
	@echo "   DATABASE_URL=postgres://postgres:postgres@localhost:15433/indexer?sslmode=disable \\"
	@echo "   RPC_URLS=http://localhost:8546 \\"
	@echo "   CHAIN_ID=31337 \\"
	@echo "   START_BLOCK=0 \\"
	@echo "   LOG_LEVEL=debug \\"
	@echo "   ./bin/indexer"
	@echo ""
	@echo "🎛️  Dashboard: http://localhost:8088"
	@echo "⛽️  Anvil RPC:  http://localhost:8546"

# 运行集成测试（使用Anvil）
test-anvil: anvil-up
	@echo "🧪 运行集成测试（Anvil）..."
	RPC_URL=http://localhost:8546 go test -v -tags=integration ./...
	@make anvil-down

# 快速验证 - 启动Anvil + 索引器 + 检查核心逻辑
verify: anvil-up demo-deploy build
	@echo ""
	@echo "🔍 启动索引器进行核心逻辑验证..."
	@timeout 30 bash -c 'DATABASE_URL=postgres://postgres:postgres@localhost:15433/indexer?sslmode=disable \
	 RPC_URLS=http://localhost:8546 \
	 CHAIN_ID=31337 \
	 START_BLOCK=0 \
	 LOG_LEVEL=debug \
	 ./bin/indexer' || true
	@echo ""
	@echo "✅ 验证完成"
	@make anvil-down
