.PHONY: build run clean vet lint test migrate-up migrate-down docker-up docker-down

# Build the indexer binary
build:
	go build -o indexer ./cmd/indexer/main.go

# Run the indexer (requires .env file)
run:
	go run ./cmd/indexer/main.go

# Clean build artifacts
clean:
	rm -f indexer
	go clean

# Run go vet
vet:
	go vet ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run

# Run tests
test:
	go test -v ./...

# Download dependencies
deps:
	go mod download
	go mod tidy

# Database migrations (requires golang-migrate)
DB_URL=postgres://postgres:postgres@localhost:5432/indexer?sslmode=disable

migrate-up:
	migrate -path migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path migrations -database "$(DB_URL)" down

# Docker operations
docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

# Full dev setup
dev-setup: docker-up migrate-up
	@echo "Development environment ready!"
	@echo "Copy .env.example to .env and update RPC_URL"
