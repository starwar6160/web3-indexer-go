# --- 质量保证流 (QA & Testing) ---

.PHONY: test test-api check

# Integration Test: API Logic Guards (Python-based)
test-api:
	@echo "🧪 Running API Logic Integration Tests..."
	@if [ -x "./venv/bin/pip" ]; then \
		echo "🐍 Using project virtual environment (./venv)..."; \
		./venv/bin/pip install -q pytest requests; \
		INDEXER_API_URL="http://localhost:8081/api" ./venv/bin/pytest tests/test_api_logic.py -v -s || (echo "❌ API Logic Check Failed!"; exit 1); \
	elif [ -x "/home/ubuntu/venv/bin/pip" ]; then \
		echo "🐍 Using home virtual environment (~/venv)..."; \
		/home/ubuntu/venv/bin/pip install -q pytest requests; \
		INDEXER_API_URL="http://localhost:8081/api" /home/ubuntu/venv/bin/pytest tests/test_api_logic.py -v -s || (echo "❌ API Logic Check Failed!"; exit 1); \
	else \
		echo "⚠️  No full venv found, falling back to system python..."; \
		pip3 install -q pytest requests || echo "⚠️ pip3 install failed, trying to run anyway..."; \
		INDEXER_API_URL="http://localhost:8081/api" pytest tests/test_api_logic.py -v -s || (echo "❌ API Logic Check Failed!"; exit 1); \
	fi
	@echo "✅ All API Logic Guards Passed."

test:
	@echo "🧪 Running Go unit tests..."
	go test -v ./internal/...

test-integration:
	@echo "🧪 Running Industrial Grade Integration Tests..."
	go test -v -tags=integration ./internal/engine

check: qa test test-integration test-api
	@echo "✅ All quality gates passed!"
