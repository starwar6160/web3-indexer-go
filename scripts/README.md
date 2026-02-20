# Environment Sanitizer Script

This script automatically detects the runtime environment (Anvil local vs Sepolia testnet) and ensures `APP_MODE` is set correctly to prevent performance throttling due to misidentification.

## Quick Start

```bash
# Source the script in your shell (exports APP_MODE)
source ./scripts/env_sanitizer.sh

# Or run it directly
./scripts/env_sanitizer.sh

# Then start the indexer
make run-8082
```

## Docker Integration

Add to your Dockerfile or docker-compose.yml:

```dockerfile
# In Dockerfile
COPY scripts/env_sanitizer.sh /app/
RUN chmod +x /app/env_sanitizer.sh
ENTRYPOINT ["/app/env_sanitizer.sh"]
CMD ["./indexer"]
```

## What It Does

1. **Detects RPC endpoint** from `RPC_URL` or `RPC_URLS` env vars
2. **Tests connectivity** to common Anvil ports (8545, 8546, 8555)
3. **Fetches Chain ID** to confirm network type:
   - `31337` = Anvil (local)
   - `11155111` = Sepolia (testnet)
   - `1` = Mainnet
4. **Measures response time** (< 50ms suggests local Anvil)
5. **Exports correct APP_MODE**:
   - `EPHEMERAL_ANVIL` for local development
   - `PERSISTENT_TESTNET` for testnet/production

## Detection Logic

| Condition | Detected As |
|-----------|-------------|
| URL contains `localhost`, `127.0.0.1`, `:8545`, `:8546`, or `anvil` | Anvil |
| Chain ID = 31337 | Anvil |
| Response time < 50ms | Anvil |
| Chain ID = 11155111 | Testnet |
| No RPC URL found | Defaults to Anvil (for demo2) |

## Example Output

### Anvil Detection
```
[INFO] 🔍 环境配置自检启动...
[INFO] 目标 RPC 端点: http://localhost:8545
[INFO] 检测到 Chain ID: 31337
[OK] ✅ Chain ID 31337 确认是 Anvil 本地网络
[OK] 🚀 识别为 Anvil 本地环境
[INFO] 设置 APP_MODE=EPHEMERAL_ANVIL

💡 调优建议:
   - 使用 Beast 模式: curl -X POST http://localhost:8082/debug/hotune/preset -d '{"mode":"BEAST"}'
   - 查看状态: curl http://localhost:8082/debug/snapshot
```

### Testnet Detection
```
[INFO] 🔍 环境配置自检启动...
[INFO] 目标 RPC 端点: https://sepolia.infura.io/v3/xxx
[INFO] 检测到 Chain ID: 11155111
[WARN] ⚠️  Chain ID 11155111 是 Sepolia 测试网
[INFO] 🛡️ 识别为测试网/生产环境
[INFO] 设置 APP_MODE=PERSISTENT_TESTNET

💡 保守模式建议:
   - 系统将使用 2 QPS 的谨慎限流
   - 背压阈值设置为 100 块
```

## Troubleshooting

### "无法检测到活跃的 RPC 端点"
- Ensure Anvil is running: `anvil --fork-url ...`
- Check RPC_URL is set: `echo $RPC_URL`
- Test connectivity manually: `curl http://localhost:8545 -X POST -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'`

### "APP_MODE 与检测结果不符"
The script will automatically override incorrect APP_MODE settings. This prevents the "PERSISTENT_TESTNET" misidentification issue that causes BPS=2 throttling.

## Integration with Hot-Tune API

After running this script and starting the indexer, use the Hot-Tune API for dynamic performance adjustment:

```bash
# Check current config
curl http://localhost:8082/debug/hotune/status

# Apply Beast mode (5000 QPS)
curl -X POST http://localhost:8082/debug/hotune/preset \
  -H "Content-Type: application/json" \
  -d '{"mode":"BEAST"}'

# Custom tuning
curl -X POST http://localhost:8082/debug/hotune/apply \
  -H "Content-Type: application/json" \
  -d '{"rpc_qps":1000,"rpc_burst":100}'
```

## Why This Matters

If the system misidentifies Anvil as Testnet:
- Rate limit drops from **1000 QPS** to **2 QPS**
- Backpressure threshold drops from **5000 blocks** to **100 blocks**
- BPS drops from **300+** to **2**
- 5600U CPU stays at **0%** instead of **60%**

This script ensures your hardware is fully utilized in local development while remaining conservative in production.
