# --- 工业级蓝绿部署/镜像流转流 (Docker Staging-to-Production) ---

.PHONY: a1 a2 test-a1 test-a2 test-debug stop-all infra-up clean-testnet

IMAGE_NAME=web3-indexer-go
STAGING_TAG=latest
STABLE_TAG=stable

infra-up:
	@echo "📦 Starting infrastructure (DB, Grafana, Prometheus, Gateway)..."
	@docker compose -f docker-compose.infra.yml up -d

# --- 1. 开发与测试阶段 (Staging) ---

test-a1: infra-up
	@echo "🛠️  构建并部署到测试环境 (Sepolia Staging)..."
	docker build -t $(IMAGE_NAME):$(STAGING_TAG) .
	docker stop web3-sepolia-staging || true
	docker rm web3-sepolia-staging || true
	# 使用 .env.testnet 中的商业 RPC
	@set -a; . ./.env.testnet; set +a; \
	docker run -d --name web3-sepolia-staging \
		--network host \
		--restart always \
		-e PORT=8091 \
		-e RPC_URLS="$$RPC_URLS" \
		-e CHAIN_ID=11155111 \
		-e DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@localhost:15432/web3_sepolia?sslmode=disable" \
		-e APP_TITLE="🧪 SEP-STAGING (8091)" \
		$(IMAGE_NAME):$(STAGING_TAG)
	@echo "✅ Staging Sepolia live on http://localhost:8091"

test-a2: infra-up
	@echo "🛠️  构建并部署到测试环境 (Anvil Staging)..."
	docker build -t $(IMAGE_NAME):$(STAGING_TAG) .
	docker stop web3-anvil-staging || true
	docker rm web3-anvil-staging || true
	docker run -d --name web3-anvil-staging \
		--network host \
		--restart always \
		-e PORT=8092 \
		-e RPC_URLS="http://localhost:8545" \
		-e CHAIN_ID=31337 \
		-e DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@localhost:15432/web3_demo?sslmode=disable" \
		-e APP_TITLE="🧪 ANVIL-STAGING (8092)" \
		-e DEMO_MODE=true \
		$(IMAGE_NAME):$(STAGING_TAG)
	@echo "✅ Staging Anvil live on http://localhost:8092"

test-debug: infra-up
	@echo "🛠️  构建并部署到测试环境 (Debug Staging)..."
	docker build -t $(IMAGE_NAME):$(STAGING_TAG) .
	docker stop web3-debug-staging || true
	docker rm web3-debug-staging || true
	docker run -d --name web3-debug-staging \
		--network host \
		--restart always \
		-e PORT=8093 \
		-e APP_TITLE="🧪 DEBUG-STAGING (8093)" \
		$(IMAGE_NAME):$(STAGING_TAG)
	@echo "✅ Staging Debug live on http://localhost:8093"

# --- 2. 生产晋升阶段 (Production) ---

a1: a1-pre-flight infra-up
	@echo "🚀 晋升测试版镜像到稳定版 8081 (Sepolia Stable)..."
	docker tag $(IMAGE_NAME):$(STAGING_TAG) $(IMAGE_NAME):$(STABLE_TAG)
	@set -a; . ./.env.testnet; set +a; \
	docker compose -f docker-compose.testnet.yml up -d --no-build
	@echo "✅ Sepolia Stable updated. Downtime < 2s (handled by Nginx Gateway)"

a2: infra-up
	@echo "🚀 晋升测试版镜像到稳定版 8082 (Anvil Stable)..."
	docker tag $(IMAGE_NAME):$(STAGING_TAG) $(IMAGE_NAME):$(STABLE_TAG)
	@set -a; . ./.env.demo2; set +a; \
	COMPOSE_PROJECT_NAME=web3-demo2 docker compose up -d --no-build
	@echo "✅ Anvil Stable updated. Downtime < 2s (handled by Nginx Gateway)"

stop-all:
	@echo "🛑 Stopping all containers..."
	docker stop web3-sepolia-staging web3-anvil-staging web3-debug-staging || true
	docker rm web3-sepolia-staging web3-anvil-staging web3-debug-staging || true
	-@docker compose -f docker-compose.testnet.yml down 2>/dev/null || true
	-@COMPOSE_PROJECT_NAME=web3-demo2 docker compose down 2>/dev/null || true
	-@docker compose -f docker-compose.infra.yml down 2>/dev/null || true
	@echo "✅ All containers stopped."

clean-testnet:
	@echo "🧹 Cleaning up testnet environment..."
	docker compose -f docker-compose.testnet.yml down -v