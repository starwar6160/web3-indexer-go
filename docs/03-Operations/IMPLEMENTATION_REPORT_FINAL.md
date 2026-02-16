# Web3 Indexer Implementation Report - Final Milestone
**Date**: 2026-02-15
**Standard**: Industrial Grade Architecture (v1.0)

## 1. Network Architecture: The "Host Mode" Decision
To eliminate the networking friction between host-resident development processes (`go run`) and containerized infrastructure, the entire stack has been migrated to **Docker Host Mode**.

- **Database**: PostgreSQL moved to port `15432` to avoid conflicts with system-level Postgres (5432).
- **Monitoring**: Prometheus (`9091`) and Grafana (`4000`) now share the host network, enabling direct scraping of `localhost:8081/8082`.
- **Visibility**: Tailscale users can now reach all services directly via host IP/domain without internal Docker NAT issues.

## 2. Observability: Industrial Dashboard v2.0
The monitoring layer has been upgraded from a simple "stat-check" to a high-fidelity performance analyzer.

- **Precise E2E Latency**: Replaced estimated lag with a dedicated Go-calculated metric: `time.Since(block.Time())`.
- **RPC Heartbeat**: Active background probing of RPC nodes ensures the `RPC Health` gauge is always accurate.
- **Robust PromQL**: All panels now use `max()` and `sum()` aggregations to bypass label mismatch errors commonly found in multi-environment setups.
- **JST Alignment**: Dashboard timezones are now browser-bound, ensuring real-time alignment for the Yokohama development hub.

## 3. UI/UX: Environment Synergy
The interface now supports seamless switching between the Public Monitor and the Stress-test Lab.

- **Critical CSS Inlining**: Injected theme styles directly into HTML to bypass Cloudflare's edge caching.
- **Dynamic Iframe Logic**: Implemented JS-based resolution for Grafana iframes. It automatically switches between `localhost` and `st6160.click` based on the user's entry point, eliminating "Private Network Access" warnings.
- **Visual Branding**: 🟢 **Sepolia Live (Demo1)** and 🟣 **Local Lab (Demo2)** now have distinct, high-contrast identities.

## 4. Integrity & Operations
- **Deployment**: `scripts/deploy-production.sh` provides a single entry point for infrastructure management.
- **Data Audit**: `scripts/verify_integrity.go` enables periodic checksums of the hash chain, guaranteeing 100% data traceability.
- **Release**: `demo1.st6160.click` is now the primary public entry point via Cloudflare Tunnel.

## 5. Port Map Reference
| Service | Host Port | URL |
|---------|-----------|-----|
| Grafana | 4000 | https://grafana-demo2.st6160.click |
| Indexer (Sepolia) | 8081 | https://demo1.st6160.click |
| Indexer (Anvil) | 8082 | https://demo2.st6160.click |
| Prometheus | 9091 | http://localhost:9091 |
| PostgreSQL | 15432 | localhost:15432 (web3_indexer) |

---
*End of Report. System is now fully Operational and Production-Ready.*
