# Web3 Indexer Go - Project Context & Architecture

This document provides a comprehensive overview of the Web3 Indexer Go project, synthesized from `CLAUDE.md`, `README-WEB3-INDEXER.md`, `QUICK_START.md`, and recent architectural audits.

## 🚀 Project Overview
A production-ready Ethereum indexer designed for high-performance indexing of ERC20 transfer events. It emphasizes **SRE-level reliability**, **cost optimization**, and **data consistency**.

### Key Technical Pillars
- **3-Stage Pipeline**: Fetcher → Sequencer → Processor.
- **ACID Data Integrity**: Uses `SERIALIZABLE` PostgreSQL transactions for atomic updates of blocks, transfers, and checkpoints.
- **Smart Cost Optimization**: Distinct operating modes (Active, Idle, Watching) to minimize RPC usage.
- **Fault Tolerance**: Multi-RPC pool with automatic failover and rate limiting.

---

## 🏗️ System Architecture

### 1. Fetcher (`internal/engine/fetcher.go`)
- **Concurrency**: Worker pool (default 10) for parallel block retrieval.
- **RPC Management**: Node pool with independent Token Bucket rate limiters.
- **Control**: Support for global pause/resume during reorgs.

### 2. Sequencer (`internal/engine/sequencer.go`)
- **Ordering**: Buffers out-of-order blocks to ensure strict sequential processing.
- **Reorg Detection**: Uses parent hash verification (not just block numbers).
- **Buffer Safety**: 1000-block limit to prevent memory exhaustion (fail-fast design).

### 3. Processor (`internal/engine/processor.go`)
- **Atomic Operations**: Single transaction for block insertion, transfer events, and checkpoint updates.
- **Reorg Handling**: Shallow and deep reorg strategies.
- **Data Consistency**: Prevents "checkpoint lead" where the tracker moves ahead of physical data.

### 4. Smart State Manager (`internal/engine/state_manager.go`)
- **Active Mode**: High performance (5-minute demo window).
- **Idle Mode**: Zero RPC consumption.
- **Watching Mode**: Uses WSS subscriptions for 97% cost saving compared to polling.

---

## 🛠️ Development & Operations

### Critical Commands
- **Quick Run**: `./dev-run.sh` (Fast development loop).
- **Build**: `make build` or `go build -o indexer ./cmd/indexer/main.go`.
- **Test**: `make test` or `go test ./internal/engine/...`.
- **Setup**: `make dev-setup` (PostgreSQL + Migrations).
- **Infrastructure**: `docker compose up -d db anvil`.

### Monitoring & UI
- **Dashboard**: `http://localhost:8080/` (Real-time status, cost metrics).
- **Health Checks**: `/healthz` (JSON).
- **Metrics**: `/metrics` (Prometheus).
- **Admin API**: `POST /api/admin/start-demo`, `GET /api/admin/status`.

---

## 🧪 Recent Architectural Fixes (Feb 9, 2026)
1. **Atomic Transaction Boundary**: Fixed a critical bug where checkpoints were updated outside the data transaction. All writes now use `SERIALIZABLE` isolation.
2. **Async Scheduling**: `fetcher.Schedule()` moved to a goroutine to prevent main thread deadlocks when job buffers are full.
3. **Reorg Recovery**: Improved sequencer buffer clearing and fetcher pausing during chain reorgs.
4. **Sequencer Batching**: Implemented batch processing in Sequencer to handle high-throughput catch-up (multi-block processing in a single loop).

---

## 💡 Interview Strategy: "Why is the sync speed limited?"

**Context**: In a local demo environment (Anvil), the sync speed might appear artificially capped (e.g., 5-10 blocks/sec) compared to the theoretical max.

**The "Smart Answer"**:
> "To ensure production reliability and prevent IP bans from providers like Infura or QuickNode, I implemented a robust **Token Bucket** traffic shaper.
> 
> The current demo speed is a **deliberate 'throttling'** to showcase the system's stability under constrained bandwidth. If we were to switch to a high-performance private node, I would simply adjust the `FETCH_RATE` env var, and the system's throughput would instantly scale up by 20x, thanks to the **Pipeline Architecture** (Fetcher, Sequencer, Processor) which naturally supports massive parallelism."

---

## 📝 Code Conventions & Implementation Details
- **Uint256 Handling**: Custom type in `internal/models/types.go` supporting scientific notation (e.g., "1.5E+18") from RPC.
- **Database Schema**: `migrations/001_init.sql` includes `CASCADE` deletes on block numbers to ensure consistency.
- **Pause Mechanism**: Uses `sync.Cond` for efficient global pausing/resuming.
- **Rate Limiting**: Token Bucket per RPC node to avoid API bans.

---

## 📊 Mode Comparison

| Mode | RPC Usage | Cost Saving | Use Case |
|------|-----------|-------------|----------|
| **Active** | ~2.4M credits/day | 0% | Fast catch-up / Demo |
| **Idle** | 0 | 100% | Manual stop / Hibernation |
| **Watching**| ~0.1M credits/day | 97% | Real-time monitoring |
