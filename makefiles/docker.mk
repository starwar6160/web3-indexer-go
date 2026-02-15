# --- 稳定容器部署流 (Docker Management) ---

.PHONY: a1 a2 stop-all infra-up clean-testnet

infra-up:
	@echo "📦 Starting infrastructure (DB, Grafana, Prometheus)..."
	@docker compose -f docker-compose.infra.yml up -d --remove-orphans

a1: a1-pre-flight infra-up
	@echo "🚀 Deploying stable Sepolia instance (8081)..."
	@set -a; \
	[ -f .env.testnet.local ] && . ./.env.testnet.local; \
	set +a; \
	docker compose -p web3-testnet -f docker-compose.yml up -d --build --force-recreate
	@echo "✅ Sepolia instance live on http://localhost:8081"

a2: infra-up
	@echo "🚀 Deploying stable Anvil instance (8080)..."
	@set -a; \
	[ -f .env.demo2 ] && . ./.env.demo2; \
	set +a; \
	docker compose -p web3-demo2 -f docker-compose.yml --profile demo up -d --build --force-recreate
	@echo "✅ Anvil instance live on http://localhost:8080"

stop-all:
	@echo "🛑 Stopping all containers..."
	-@docker compose -p web3-testnet down 2>/dev/null || true
	-@docker compose -p web3-demo2 down 2>/dev/null || true
	-@docker compose -f docker-compose.infra.yml down 2>/dev/null || true
	@echo "✅ All containers stopped."

clean-testnet:
	@echo "🧹 Cleaning up testnet environment..."
	docker compose -p web3-testnet down -v