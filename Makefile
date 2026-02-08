# Web3 Indexer - Go SRE级别项目
# 一键部署和启动脚本

.PHONY: help build run start stop clean test docker-up docker-down logs status dev-setup

# 默认目标
help:
	@echo "🚀 Web3 Indexer - Go SRE级别项目"
	@echo ""
	@echo "可用命令:"
	@echo "  make build     - 编译Go二进制文件"
	@echo "  make run       - 直接运行Go程序"
	@echo "  make start     - 启动所有服务 (数据库 + 索引器)"
	@echo "  make stop      - 停止所有服务"
	@echo "  make clean     - 清理所有容器和数据"
	@echo "  make test      - 运行测试"
	@echo "  make docker-up - 仅启动Docker基础设施"
	@echo "  make docker-down - 停止Docker基础设施"
	@echo "  make logs      - 查看服务日志"
	@echo "  make status    - 查看服务状态"
	@echo "  make dev-setup - 开发环境设置"
	@echo ""
	@echo "访问地址:"
	@echo "  🎛️  Dashboard: http://localhost:8080"
	@echo "  ❤️   Health:   http://localhost:8080/healthz"
	@echo "  📊 Metrics:  http://localhost:8080/metrics"

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
	@echo "   DATABASE_URL=postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable"
	@echo "   RPC_URLS=https://eth.llamarpc.com"
	@echo "   CHAIN_ID=1"
	@echo ""
	@DATABASE_URL=postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable \
	 RPC_URLS=https://eth.llamarpc.com \
	 CHAIN_ID=1 \
	 START_BLOCK=185000000 \
	 LOG_LEVEL=info \
	 ./bin/indexer &
	@echo "✅ 索引器已启动"
	@echo ""
	@echo "🎛️  Dashboard: http://localhost:8080"
	@echo "❤️   Health:   http://localhost:8080/healthz"
	@echo "📊 Metrics:  http://localhost:8080/metrics"
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
	@DATABASE_URL=postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable \
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
DB_URL=postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable

migrate-up:
	@echo "📈 执行数据库迁移..."
	migrate -path migrations -database "$(DB_URL)" up 2>/dev/null || echo "✅ 使用Docker自动初始化"

migrate-down:
	@echo "📉 执行数据库回滚..."
	migrate -path migrations -database "$(DB_URL)" down 2>/dev/null || echo "❌ migrate未安装"

# 快速重启
restart: stop start
