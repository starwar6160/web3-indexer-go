# Sepolia Testnet Deployment Guide

## Overview

This guide walks you through deploying the Web3 Indexer V2 to Ethereum Sepolia testnet, transitioning from local Anvil development to a real public blockchain network.

## Prerequisites

1. **RPC Provider Account**
   - Alchemy (recommended): https://www.alchemy.com/
   - Infura: https://www.infura.io/
   - QuickNode: https://www.quicknode.pro/
   - Or use public RPC: https://rpc.sepolia.org

2. **Database**
   - PostgreSQL 12+ running locally or in cloud
   - Connection string format: `postgres://user:pass@host:port/dbname`

3. **Go 1.21+**
   - Verify: `go version`

## Step 1: Get RPC Credentials

### Alchemy (Recommended)
1. Sign up at https://www.alchemy.com/
2. Create a new app, select "Ethereum" → "Sepolia"
3. Copy the HTTPS URL (e.g., `https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY`)
4. Optionally copy the WSS URL for real-time monitoring

### Infura
1. Sign up at https://www.infura.io/
2. Create a new project, select "Ethereum"
3. Switch to "Sepolia" network
4. Copy the HTTPS URL (e.g., `https://sepolia.infura.io/v3/YOUR_KEY`)

## Step 2: Configure Environment

### Create `.env` file from template
```bash
cp .env.example .env
```

### Edit `.env` with your credentials
```env
# Database
DATABASE_URL=postgres://user:password@localhost:5432/indexer

# RPC Endpoints (multi-provider for failover)
RPC_URLS=https://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY,https://sepolia.infura.io/v3/YOUR_INFURA_KEY,https://rpc.sepolia.org

# Optional: WebSocket for real-time monitoring
WSS_URL=wss://eth-sepolia.g.alchemy.com/v2/YOUR_ALCHEMY_KEY

# Chain Configuration
CHAIN_ID=11155111  # Sepolia testnet

# Sync Configuration
START_BLOCK=5000000  # Adjust based on contract deployment
CONFIRMATION_DEPTH=6  # Testnet: 2-6 blocks
BLOCK_BATCH_SIZE=1000
POLL_INTERVAL=12000  # Sepolia ~12s per block

# Logging
LOG_LEVEL=info
```

## Step 3: Database Setup

### Create database
```bash
createdb indexer
```

### Run migrations
```bash
psql -U postgres -d indexer -f migrations/001_init.sql
```

### Verify schema
```bash
psql -U postgres -d indexer -c "\dt"
```

## Step 4: Build and Run

### Build the indexer
```bash
go build -o indexer ./cmd/indexer
```

### Run the indexer
```bash
./indexer
```

### Expected output
```
INFO  starting_web3_indexer version=V2 mode=production_ready
INFO  rpc_providers_configured provider_count=3 providers=https://eth-sepolia.g.alchemy.com/v2/... | https://sepolia.infura.io/v3/... | https://rpc.sepolia.org
INFO  database_connected max_open_conns=25 max_idle_conns=10
INFO  rpc_pool_initialized healthy_nodes=3
INFO  latest_block_fetched latest_block=5123456 start_block=5000000 blocks_behind=123456
INFO  blocks_scheduled start_block=5000000 end_block=5001000 mode=incremental_sync
INFO  sequencer_started mode=ordered_processing expected_block=5000000
INFO  http_server_started port=8080 health_endpoint=http://localhost:8080/healthz metrics_endpoint=http://localhost:8080/metrics
```

## Step 5: Monitor Health

### Check health endpoint
```bash
curl http://localhost:8080/healthz
```

### View Prometheus metrics
```bash
curl http://localhost:8080/metrics | grep indexer_
```

### Monitor logs
```bash
# Watch real-time logs
tail -f indexer.log

# Filter by level
grep "ERROR\|WARN" indexer.log
```

## Key Differences from Anvil

### 1. Block Time
- **Anvil**: Instant (0s)
- **Sepolia**: ~12 seconds per block
- **Adjustment**: Set `POLL_INTERVAL=12000`

### 2. Confirmation Depth
- **Anvil**: Immediate finality
- **Sepolia**: 2-6 blocks recommended
- **Adjustment**: Set `CONFIRMATION_DEPTH=6`

### 3. Rate Limiting
- **Anvil**: Unlimited
- **Sepolia**: Provider-dependent (Alchemy: ~330 CUPS)
- **Adjustment**: Use multi-provider failover, reduce `MAX_CONCURRENCY` if needed

### 4. Data Availability
- **Anvil**: Only your test transactions
- **Sepolia**: Real transactions from entire network
- **Benefit**: Continuous data flow without manual setup

## Troubleshooting

### "no healthy RPC nodes available"
- Verify RPC URLs are correct
- Check API key quotas in provider dashboard
- Add more RPC providers to `RPC_URLS`

### "block range too large"
- Indexer automatically handles 2000-block limit
- If still failing, reduce `BLOCK_BATCH_SIZE`

### "connection refused" to database
- Verify PostgreSQL is running
- Check `DATABASE_URL` connection string
- Ensure database exists: `createdb indexer`

### High sync lag
- Reduce `BLOCK_BATCH_SIZE` to lower memory usage
- Increase `POLL_INTERVAL` to reduce RPC load
- Check RPC provider health dashboard

## Performance Tuning

### For faster sync (more memory)
```env
BLOCK_BATCH_SIZE=2000
MAX_CONCURRENCY=20
DB_MAX_OPEN_CONNS=50
```

### For lower resource usage (slower sync)
```env
BLOCK_BATCH_SIZE=100
MAX_CONCURRENCY=5
DB_MAX_OPEN_CONNS=10
```

### For rate-limited providers
```env
BLOCK_BATCH_SIZE=500
MAX_CONCURRENCY=5
POLL_INTERVAL=15000
```

## Production Deployment

### Docker Deployment
```bash
docker build -t web3-indexer .
docker run -d \
  --name web3-indexer \
  -e DATABASE_URL="postgres://..." \
  -e RPC_URLS="https://..." \
  -e CHAIN_ID=11155111 \
  -p 8080:8080 \
  web3-indexer
```

### Kubernetes Deployment
See `k8s/deployment.yaml` for full manifest

### Cloud Providers
- **AWS**: Deploy to EC2 with RDS PostgreSQL
- **DigitalOcean**: App Platform + Managed Database
- **Heroku**: Not recommended (no persistent storage)

## Monitoring Dashboard

The indexer exposes metrics at `http://localhost:8080/metrics`:

```
# Key metrics to monitor
indexer_blocks_processed_total  # Total blocks processed
indexer_blocks_processed_duration_seconds  # Processing latency
indexer_transfers_indexed_total  # Total transfers indexed
indexer_sync_lag_blocks  # Current lag behind latest block
indexer_rpc_requests_total  # Total RPC requests
indexer_rpc_errors_total  # Failed RPC requests
indexer_reorg_detected_total  # Reorg events detected
```

## Next Steps

1. **Verify sync**: Wait for 1000+ blocks to be indexed
2. **Check data**: Query transfers table for real data
3. **Monitor**: Watch metrics and logs for 24+ hours
4. **Optimize**: Adjust batch size and concurrency based on performance
5. **Deploy**: Move to production infrastructure

## Support

For issues or questions:
1. Check logs: `grep ERROR indexer.log`
2. Verify RPC health: `curl https://your-rpc-url -X POST -H "Content-Type: application/json" --data '{"method":"eth_blockNumber","params":[],"id":1,"jsonrpc":"2.0"}'`
3. Check database: `psql -d indexer -c "SELECT COUNT(*) FROM blocks;"`
