# Improvements Report - 2026-02-27 (Stable Version)

## 1. Infrastructure & Environment Stabilization
**Goal**: Resolve port conflicts and fix incorrect database configurations for the stable demo environment.

### Changes:
- **Port Conflict Resolution**: Identified and removed stale `web3-indexer-app` container that was blocking port `8082`.
- **Database Configuration Fix**: 
    - Updated `configs/env/config.demo.golden.env` to point to the correct infrastructure Postgres port (`5432` instead of `15432`).
    - Corrected database credentials (`uWNZugjBqixf8dxC`).
- **Networking Standard**: Enforced `network_mode: host` in `docker-compose.yml` to ensure seamless communication with local Anvil and Postgres instances.

## 2. Rich UI Data Simulation
**Goal**: Enhance the dashboard visual experience by providing diverse transaction data in simulation mode.

### Changes:
- **Activity Type Randomization**: In Anvil mode, synthetic transfers now randomly alternate between `SWAP`, `MINT`, `BURN`, `LIQUIDITY`, `FAUCET_CLAIM`, and `TRANSFER`.
- **Entity & Amount Variety**: 
    - Added a pool of 5 different Anvil addresses to simulate multi-user activity.
    - Implemented multi-scale amount generation (Units, 1M, 1B) based on block number.
- **Persistence Fix**: Updated `Repository.SaveTransfer` to correctly persist `symbol` and `activity_type` fields to PostgreSQL.

## 3. UI Resilience & Accuracy
**Goal**: Ensure the UI displays correct statistics even during transient database unavailability.

### Changes:
- **Shadow Snapshot Fallback**: Updated `GetUIStatus` projection logic to use Orchestrator memory counters for `Total Transfers` if the database query returns zero or fails.
- **Sync Progress Optimization**: Fixed calculation logic for UI progress bars to better reflect the real-time gap between `Latest Block` and `Synced Cursor`.

## 4. Observability Migration (Prometheus Native)
**Goal**: Shift Grafana monitoring from SQL-polling to real-time internal routine metrics.

### Changes:
- **New Internal Metrics**:
    - `indexer_system_state_code`: Real-time SSOT state (Running, Throttled, etc.).
    - `indexer_fetcher_jobs_queue_depth`: Monitor input pressure.
    - `indexer_fetcher_results_depth`: Monitor processing backpressure.
- **Dashboard Re-engineering**:
    - Migrated all panels in `Web3-Indexer-Dashboard.json` to use the **Prometheus** datasource.
    - Added **Runtime Health** panels (Go Goroutines, Memory Alloc/Heap/Sys).
    - Added **Pipeline Depth** visualization for real-time bottleneck detection.
- **Security & Efficiency**: Removed SQL-based table panels to reduce database load and improve dashboard responsiveness.

---
**Status**: Stable Release `v2026.02.27-ui-final` deployed and verified.
**Author**: Gemini CLI
**Date**: 2026-02-27
