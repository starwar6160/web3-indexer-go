# --- 极速调试流 (Local Development) ---

.PHONY: b1 b2 stop-dev

# 调试模式 B1 (Sepolia / 8081)
b1:
	@echo "🛑 Clearing port 8081 (Sepolia)..."
	-@docker stop web3-testnet-app 2>/dev/null || true
	-@fuser -k 8081/tcp 2>/dev/null || true
	@echo "🚀 Starting Sepolia Indexer (Local go run)..."
	@set -a; \
	[ -f .env.testnet.local ] && . ./.env.testnet.local; \
	export DATABASE_URL=$$(echo $$DATABASE_URL | sed 's/@.*:5432/@127.0.0.1:15433/'); \
	export PORT=8081; export DEMO_MODE=false; export IS_TESTNET=true; \
	go run ./cmd/indexer

# 调试模式 B2 (Anvil / 8082)
b2:
	@echo "🛑 Clearing port 8082 (Anvil)..."
	-@docker stop web3-demo2-app 2>/dev/null || true
	-@fuser -k 8082/tcp 2>/dev/null || true
	@echo "🚀 Starting Anvil Indexer (Local go run)..."
	@set -a; \
	[ -f .env.demo2 ] && . ./.env.demo2; \
	export DATABASE_URL=$$(echo $$DATABASE_URL | sed 's/@.*:5432/@127.0.0.1:15434/'); \
	export PORT=8082; export DEMO_MODE=true; export IS_TESTNET=false; \
	go run ./cmd/indexer

stop-dev:
	@echo "🛑 Cleaning up development ports..."
	-fuser -k 8081/tcp 2>/dev/null || true
	-fuser -k 8082/tcp 2>/dev/null || true
	@echo "✅ Local development cleaned up."