PROJECT_NAME=web3-indexer
# --- 工业级双模流水线 (Local Dev + Docker Production) ---

.PHONY: a1 a2 test-a1 test-a2 test-debug stop-all infra-up clean-testnet clean-state

IMAGE_NAME=web3-indexer-go
STAGING_TAG=latest
STABLE_TAG=stable

INFRA_COMPOSE=configs/docker/docker-compose.infra.yml
TESTNET_COMPOSE=configs/docker/docker-compose.testnet.yml

# 🔥 新增：无状态清理目标
clean-state:
	@echo "🔄 完全清理系统状态（无状态模式）..."
	@./scripts/clean-state.sh

infra-up:
	@echo "📦 Starting infrastructure (DB, Grafana, Prometheus)..."
	@docker compose -p $(PROJECT_NAME) -f $(INFRA_COMPOSE) up -d

# --- 1. 极速开发阶段 (Local Hot-Run) ---
# 不需要构建镜像，直接利用 3800X 的性能秒开

test-a1: infra-up
	@echo "🛑 [LOCAL] 正在停止旧的 8091 实例..."
	@lsof -ti:8091 | xargs kill -9 2>/dev/null || true
	@sleep 1
	@echo "🚀 [LOCAL] 正在确保数据库 Schema 已就绪..."
	@PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_sepolia -f scripts/db/init-db.sql >/dev/null 2>&1 || true
	@echo "🚀 [LOCAL] 正在以 Sepolia 配置直接启动..."
	@docker stop web3-sepolia-staging 2>/dev/null || true
	@set -a; . configs/env/.env.testnet; set +a; \
	PORT=8091 \
	DEMO_MODE=false \
	ENABLE_SIMULATOR=false \
	RPC_RATE_LIMIT=10 \
	FETCH_CONCURRENCY=3 \
	APP_TITLE="🚀 SEP-LOCAL (8091)" \
	DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@127.0.0.1:15432/web3_sepolia?sslmode=disable" \
	go run cmd/indexer/*.go

test-a2: infra-up
	@echo "🛑 [LOCAL] 正在停止旧的 8092 实例..."
	@lsof -ti:8092 | xargs kill -9 2>/dev/null || true
	@sleep 1
	@echo "🔍 [LOCAL] 检测 Anvil 当前高度..."
	@ANVIL_HEIGHT=$$(scripts/get-anvil-height.sh); \
	echo "📊 Anvil 当前高度：$$ANVIL_HEIGHT"; \
	echo "🚀 [LOCAL] 正在确保数据库 Schema 已就绪..."; \
	PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -f scripts/db/001_init.sql >/dev/null 2>&1 || true; \
	PGPASSWORD=W3b3_Idx_Secur3_2026_Sec psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -f scripts/db/002_visitor_stats.sql >/dev/null 2>&1 || true; \
	echo "🚀 [LOCAL] 正在以 Anvil 配置直接启动..."; \
	docker stop web3-anvil-staging 2>/dev/null || true; \
	set -a; . configs/env/.env.demo2; set +a; \
	PORT=8092 \
	RPC_URLS="http://127.0.0.1:8545" \
	CHAIN_ID=31337 \
	DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@127.0.0.1:15432/web3_demo?sslmode=disable" \
	APP_TITLE="🧪 ANVIL-LOCAL (8092) [Latest:$$ANVIL_HEIGHT]" \
	DEMO_MODE=false \
	ENABLE_SIMULATOR=true \
	RPC_RATE_LIMIT=500 \
	go run cmd/indexer/*.go

# --- 2. 生产晋升阶段 (Docker Deployment) ---

a1: a1-pre-flight infra-up
	@echo "📦 [DOCKER] 构建并部署 Sepolia 正式版 (8081)..."
	docker build -t $(IMAGE_NAME):$(STABLE_TAG) .
	docker stop web3-testnet-app || true
	docker rm web3-testnet-app || true
	@set -a; . configs/env/.env.testnet; set +a; \
	docker compose -p $(PROJECT_NAME) -f $(TESTNET_COMPOSE) up -d --no-build
	@echo "✅ Sepolia Stable deployed via Docker."

a2: infra-up
	@echo "📦 [DOCKER] 构建并部署 Anvil 正式版 (8082)..."
	docker build -t $(IMAGE_NAME):$(STABLE_TAG) .
	docker stop web3-demo2-app || true
	docker rm web3-demo2-app || true
	@set -a; . configs/env/.env.demo2; set +a; \
	COMPOSE_PROJECT_NAME=web3-demo2 docker compose -p $(PROJECT_NAME) -f configs/docker/docker-compose.yml up -d --no-build
	@echo "✅ Anvil Stable deployed via Docker."

stop-all:
	@echo "🛑 Stopping all containers..."
	docker stop web3-sepolia-staging web3-anvil-staging web3-debug-staging || true
	docker rm web3-sepolia-staging web3-anvil-staging web3-debug-staging || true
	-@docker compose -p $(PROJECT_NAME) -f $(TESTNET_COMPOSE) down 2>/dev/null || true
	-@COMPOSE_PROJECT_NAME=web3-demo2 docker compose -p $(PROJECT_NAME) -f configs/docker/docker-compose.yml down 2>/dev/null || true
	-@docker compose -p $(PROJECT_NAME) -f $(INFRA_COMPOSE) down 2>/dev/null || true
	@echo "✅ All containers stopped."

# --- Anvil Disk Management ---
.PHONY: check-disk-space anvil-emergency-cleanup anvil-disk-usage

check-disk-space:
	@echo "💾 Checking disk space..."
	@bash scripts/infra/disk-monitor.sh

anvil-emergency-cleanup:
	@echo "🚨 Running Anvil emergency cleanup..."
	@bash scripts/infra/anvil-emergency-cleanup.sh

anvil-disk-usage:
	@echo "📊 Anvil container disk usage breakdown:"
	@echo "Container Virtual Size:"
	@docker ps --filter "name=anvil" --format "table {{.Names}}\t{{.Size}}" 2>/dev/null || echo "No Anvil containers running"
	@echo ""
	@echo "Internal tmpfs Usage:"
	@$(eval ANVIL_CONTAINER := $(shell docker ps --format '{{.Names}}' | grep anvil | head -1))
	@if [ -n "$(ANVIL_CONTAINER)" ]; then \
		docker exec $(ANVIL_CONTAINER) du -sh /home/foundry/.foundry/anvil/tmp 2>/dev/null || echo "Container not accessible"; \
		docker exec $(ANVIL_CONTAINER) df -h /home/foundry/.foundry/anvil/tmp 2>/dev/null || echo "tmpfs not found"; \
	else \
		echo "No Anvil container found"; \
	fi
	@echo ""
	@echo "Snapshot Count:"
	@if [ -n "$(ANVIL_CONTAINER)" ]; then \
		docker exec $(ANVIL_CONTAINER) find /home/foundry/.foundry/anvil/tmp -name "anvil-state-*" -type d 2>/dev/null | wc -l || echo "0"; \
	else \
		echo "N/A"; \
	fi

