# --- 极速调试流 (Local Development) ---

.PHONY: b1 b2 repair stop-dev dev-stable verify-no-sleep

# 🚀 Dev Stable Mode: Never Hibernate for 5600U Infinite Processing
dev-stable:
	@echo "🔥 Starting LOCAL STABLE (8082) in NEVER HIBERNATE mode..."
	@echo "🛑 Clearing port 8082..."
	-@docker stop web3-demo2-app 2>/dev/null || true
	-@fuser -k 8082/tcp 2>/dev/null || true
	@sleep 1
	@echo ""
	@echo "Configuration:"
	@echo "  • Chain: Anvil (31337)"
	@echo "  • Mode: NEVER HIBERNATE 🔥"
	@echo "  • RPS: Unlimited (500+)"
	@echo "  • CPU: 100% available"
	@echo "  • Memory: Hot-Vault retention"
	@echo ""
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	export DATABASE_URL="postgres://postgres:W3b3_Idx_Secur3_2026_Sec@localhost:15432/web3_demo?sslmode=disable"; \
	export PORT=8082; \
	export CHAIN_ID=31337; \
	export APP_TITLE="🔥 LOCAL STABLE - NEVER HIBERNATE (8082)"; \
	export DEMO_MODE=true; \
	export ENABLE_SIMULATOR=true; \
	export RPC_RATE_LIMIT=500; \
	export FETCH_CONCURRENCY=4; \
	export IS_TESTNET=false; \
	go run ./cmd/indexer

# Verify No-Sleep Mode
verify-no-sleep:
	@echo "🔍 Verifying No-Sleep Mode..."
	@bash scripts/verify-no-sleep.sh 8082

# 调试模式 B1 (Sepolia / 8081)
b1:
	@echo "🛑 Clearing port 8081 (Sepolia)..."
	-@docker stop web3-testnet-app 2>/dev/null || true
	-@fuser -k 8081/tcp 2>/dev/null || true
	@echo "🚀 Starting Sepolia Indexer (Local go run with RESET)..."
	@set -a; \
	[ -f .env.testnet.local ] && . ./.env.testnet.local; \
	export DATABASE_URL=$$(echo $$DATABASE_URL | sed 's/@.*:5432/@127.0.0.1:15433/'); \
	export PORT=8081; export DEMO_MODE=false; export IS_TESTNET=true; \
	go run ./cmd/indexer --reset

# 调试模式 B2 (Anvil / 8082)
b2:
	@echo "🛑 Clearing port 8082 (Anvil)..."
	-@docker stop web3-demo2-app 2>/dev/null || true
	-@fuser -k 8082/tcp 2>/dev/null || true
	@echo "🚀 Starting Anvil Indexer (Local go run with RESET)..."
	@set -a; \
	[ -f .env.demo2 ] && . ./.env.demo2; \
	export DATABASE_URL=$$(echo $$DATABASE_URL | sed 's/@.*:5432/@127.0.0.1:15434/'); \
	export PORT=8082; export DEMO_MODE=true; export IS_TESTNET=false; \
	go run ./cmd/indexer --reset

# 异步哈希修复 (针对 Sepolia)
repair:
	@echo "🛠️  Starting asynchronous hash chain repair (Sepolia)..."
	@set -a; [ -f .env.testnet.local ] && . ./.env.testnet.local; set +a; \
	export DATABASE_URL=$$(echo $$DATABASE_URL | sed 's/@.*:5432/@127.0.0.1:15433/'); \
	if [ -x "./venv/bin/python3" ]; then \
		./venv/bin/python3 scripts/repair_hashes.py; \
	elif [ -x "/home/ubuntu/venv/bin/python3" ]; then \
		/home/ubuntu/venv/bin/python3 scripts/repair_hashes.py; \
	else \
		python3 scripts/repair_hashes.py; \
	fi

stop-dev:
	@echo "🛑 Cleaning up development ports..."
	-fuser -k 8081/tcp 2>/dev/null || true
	-fuser -k 8082/tcp 2>/dev/null || true
	@echo "✅ Local development cleaned up."
