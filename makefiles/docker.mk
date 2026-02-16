PROJECT_NAME=web3-indexer
# --- 工业级双模流水线 (Local Dev + Docker Production) ---

.PHONY: a1 a2 test-a1 test-a2 test-debug stop-all infra-up clean-testnet

IMAGE_NAME=web3-indexer-go
STAGING_TAG=latest
STABLE_TAG=stable

INFRA_COMPOSE=configs/docker/docker-compose.infra.yml
TESTNET_COMPOSE=configs/docker/docker-compose.testnet.yml

infra-up:
	@echo "📦 Starting infrastructure (DB, Grafana, Prometheus)..."
	@docker compose -p $(PROJECT_NAME) -f $(INFRA_COMPOSE) up -d

# --- 1. 极速开发阶段 (Local Hot-Run) ---
# 不需要构建镜像，直接利用 3800X 的性能秒开

test-a1: infra-up
	@echo "🚀 [LOCAL] 正在以 Sepolia 配置直接启动..."
	@docker stop web3-sepolia-staging 2>/dev/null || true
	@set -a; . configs/env/.env.testnet; set +a; \
	PORT=8091 \
	DEMO_MODE=false \
	APP_TITLE="🚀 SEP-LOCAL (8091)" \
	DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@127.0.0.1:15432/web3_sepolia?sslmode=disable" \
	go run cmd/indexer/*.go

test-a2: infra-up
	@echo "🚀 [LOCAL] 正在以 Anvil 配置直接启动..."
	@docker stop web3-anvil-staging 2>/dev/null || true
	@set -a; . configs/env/.env.demo2; set +a; \
	PORT=8092 \
	RPC_URLS="http://127.0.0.1:8545" \
	CHAIN_ID=31337 \
	DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@127.0.0.1:15432/web3_demo?sslmode=disable" \
	APP_TITLE="🧪 ANVIL-LOCAL (8092)" \
	DEMO_MODE=false \
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