# Industrial-Grade Web3 Indexer (Yokohama Lab)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/starwar6160/web3-indexer-go)](https://goreportcard.com/report/github.com/starwar6160/web3-indexer-go)

[🌐 **English**](./README.md) | [🏮 **中文说明**](./README_ZH.md) | [🗾 **日本語の説明**](./README_JA.md)

### 🚀 Live Demos
*   **Production (Sepolia)**: [https://demo1.st6160.click/](https://demo1.st6160.click/)
*   **Local Lab (Anvil)**: [https://demo2.st6160.click/](https://demo2.st6160.click/)

An ultra-reliable, cost-efficient Ethereum event indexer built with **Go**, **PostgreSQL**, and **Docker**.

## 🚀 Engineering Highlights

*   **Industrial-Grade Reliability**: 24/7 autonomous operation with a **Staging-to-Production** workflow and near-zero downtime deployments (< 2s window).
*   **Cost-Centric Architecture**: Integrated **Weighted Token Bucket** rate-limiter to maximize Alchemy/Infura free-tier quotas (Weighted 3:1 primary/backup ratio).
*   **429 Circuit Breaker**: Intelligent failure detection that triggers a 5-minute cooldown and automatic failover upon receiving rate-limit errors.
*   **Deterministic Security**: Startup guard with mandatory **NetworkID/ChainID verification** to prevent cross-environment database contamination.
*   **Range-Based Ingestion**: Optimized `eth_getLogs` batching (50 blocks/request) with a **Keep-alive** progress mechanism to ensure real-time UI updates even for sparse event data.
*   **Early-Bird API**: Decoupled Web server startup that opens the listener port in milliseconds, eliminating Cloudflare 502 errors during engine initialization.

## 🛠️ Tech Stack & Lab Environment

*   **Backend**: Go (Golang) + `go-ethereum`
*   **Infrastructure**: Docker (Physical isolation for Demo/Sepolia/Debug environments)
*   **Storage**: PostgreSQL (Isolated physical databases per instance)
*   **Observability**: Prometheus + Grafana (Multi-environment switchable dashboard)
*   **Lab Hardware**: AMD Ryzen 7 3800X (8C/16T), 128GB DDR4 RAM, Samsung 990 PRO 4TB NVMe

## 📦 Deployment Workflow

We use a "Staging-to-Production" flow to ensure the public demo is always stable:

1.  **Test**: Deploy to Staging (Port 8091/8092) via `make test-a1` or `make test-a2`.
2.  **Verify**: Perform smoke tests on the staging endpoint.
3.  **Promote**: Hot-swap staging image to Production (Port 8081/8082) via `make a1` or `make a2`. 
    *   *Action*: `docker tag :latest -> :stable` + `docker compose up -d --no-build`.

## 📈 Performance & Optimization

| Mode | Target Network | RPS Limit | Latency | Strategy |
| :--- | :--- | :--- | :--- | :--- |
| **Stable** | Sepolia (Testnet) | 3.5 RPS | ~12s | Weighted Multi-RPC |
| **Demo** | Anvil (Local) | 10000+ RPS | < 1s | Zero-Throttling |
| **Debug** | Sepolia (Testnet) | 5.0 RPS | ~12s | Direct Commercial RPC |

## 🔐 Cryptographic Identity

*   **Developer**: Zhou Wei (周伟) <zhouwei6160@gmail.com>
*   **Lab Location**: Yokohama, Japan (橫濱)
*   **GPG Fingerprint**: `FFA0 B998 E7AF 2A9A 9A2C 6177 F965 25FE 5857 5DCF`
*   **Verification**: All API responses are signed using **Ed25519** for end-to-end integrity.

---
© 2026 Zhou Wei. Yokohama Lab. All rights reserved.
