# Dual Instance Architecture

This project supports running multiple independent indexer instances on a single host (e.g., Ubuntu 5600U) using the same binary but different configurations.

## 1. Instance Overview

| Feature | Demo Instance (Anvil) | Testnet Instance (Sepolia) |
| --- | --- | --- |
| **Port** | 8080 | 8081 |
| **Data Source** | Local Anvil Simulator | Sepolia Public RPC |
| **Database** | `web3_indexer` | `web3_sepolia` |
| **Config File** | `.env.demo2` | `.env.testnet.local` |
| **Mode** | `DEMO_MODE=true` | `IS_TESTNET=true` |

## 2. Resource Isolation

Each instance is isolated via Docker container naming and port mapping. 

- **Demo**: `web3-indexer-anvil-app`
- **Testnet**: `web3-indexer-sepolia-app`

## 3. Database Strategy

Both instances share the same PostgreSQL service but use different database names. This minimizes overhead on hardware while ensuring zero data interference.

- **Initialization**: Databases are automatically created via `scripts/init-db.sql`.
- **Checkpointing**: Independent sync checkpoints are maintained for each chain.

## 4. Port Mapping (5600U Host)

- `:8080` -> Demo Dashboard & API
- `:8081` -> Testnet Dashboard & API
- `:4000` -> Unified Grafana Monitoring
