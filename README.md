# web3-indexer-go

Clean Architecture based Web3 indexer for EVM chains.

## Project Structure

- `cmd/indexer/` - Application entry point
- `internal/config/` - Configuration management
- `internal/database/` - Database operations
- `internal/engine/` - Core indexing logic
- `internal/models/` - Data models
- `migrations/` - SQL schema migrations

## Quick Start

```bash
go mod tidy
go run cmd/indexer/main.go
```
