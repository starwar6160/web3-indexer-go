# ==============================================================================
# Web3 Indexer 工业级控制台 (Commander)
# ==============================================================================

.PHONY: help build run air test clean demo infra-up infra-down status stress-test

# 默认目标
help:
	@echo "可用指令:"
	@echo "  make build        - 编译生产级静态二进制文件"
	@echo "  make air          - 启动热重载开发模式 (推荐)"
	@echo "  make demo         - 一键启动演示流水线 (重置环境+实时产块)"
	@echo "  make infra-up     - 启动 Docker 基础设施 (Postgres + Anvil)"
	@echo "  make infra-down   - 停止并清理基础设施"
	@echo "  make stress-test  - 启动高频压测脚本"
	@echo "  make clean        - 清理构建产物"
	@echo "  make status       - 检查容器与服务状态"
	@echo "  make pulse        - 强制激活 Anvil 自动产块 (1s/block)"

build:
	./scripts/publish.sh

pulse:
	@curl -s -H "Content-Type: application/json" -X POST --data '{"jsonrpc":"2.0","method":"evm_setIntervalMining","params":[1],"id":1}' http://127.0.0.1:8545

run:
	go run ./cmd/indexer --reset

air:
	export PATH=$(PATH):$(shell go env GOPATH)/bin && air

infra-up:
	docker compose -f docker-compose.infra.yml up -d

infra-down:
	docker compose -f docker-compose.infra.yml down -v

demo:
	./start_demo.sh

stress-test:
	python3 scripts/stress-test.py

clean:
	rm -rf bin/indexer bin/web3-indexer.service tmp/ build/

status:
	docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
	@echo "\nIndexer Log Tail:"
	tail -n 10 bin/indexer.log || true