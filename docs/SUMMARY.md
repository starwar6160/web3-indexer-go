# Documentation Index (SUMMARY.md)

Welcome to the Web3 Indexer documentation. This project is optimized for both human engineering and AI-assisted maintenance.

## 📂 01-Architecture
| Component | Path | AI Context (RAG) |
| --- | --- | --- |
| **Logic Guards** | `docs/01-Architecture/A1_ARCHITECTURE.md` | Core state-machine and sync logic. |
| **Audit Summary** | `docs/01-Architecture/ARCHITECTURE_AUDIT_FIXES.md` | Fixes for hash-self-reference and lag. |
| **Lazy Strategy** | `docs/01-Architecture/LazyIndexMode.md` | `ENABLE_ENERGY_SAVING` toggle & sleep logic. |
| **Visual Mapping** | `docs/01-Architecture/ARCHITECTURE_DIAGRAM.md` | System data flow and component decoupling. |

## 📂 03-Web3-RPC
| Provider | Path | AI Context (RAG) |
| --- | --- | --- |
| **Anvil Setup** | `docs/03-Web3-RPC/ANVIL_QUICK_START.md` | Local Ethereum simulator configuration. |
| **Sepolia Env** | `docs/03-Web3-RPC/SEPOLIA_DEPLOYMENT.md` | Public testnet endpoint and rate-limit settings. |

## 📂 04-Observability
| Layer | Path | AI Context (RAG) |
| --- | --- | --- |
| **Metrics Def** | `docs/04-Observability/REALTIME_MONITORING.md` | Prometheus metrics and E2E latency formulas. |
| **Grafana Fix** | `docs/04-Observability/GRAFANA_TUNNEL_FIX.md` | Cloudflare tunnel and dashboard access logic. |

## 📂 99-Operations
| Manual | Path | AI Context (RAG) |
| --- | --- | --- |
| **Quick Start** | `docs/99-Operations/QUICK_START.md` | One-command setup for development. |
| **Deployment** | `docs/99-Operations/DEPLOYMENT.md` | Production-grade Docker & systemd procedures. |
| **Achievements** | `docs/99-Operations/ACHIEVEMENT_SUMMARY.md` | Project milestones and 5600U performance data. |

---
*Last Updated: 2026-02-15 · Environment: Ubuntu 5600U · Status: 6-Nines Ready*