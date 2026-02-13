# 配置管理说明

本项目采用集中化配置管理，以简化部署和维护工作。

## 配置文件结构

```
config/
├── demo-config.json          # 演示模式配置
├── production-template.json  # 生产配置模板
└── ...
```

## 部署选项

### 演示模式（一键部署）
```bash
make demo
# 或
make setup-demo
```
此模式使用安全的默认配置，适合快速体验和开发测试。

### 生产部署
```bash
# 设置生产环境变量
export DATABASE_URL="postgres://user:pass@host:port/db"
export RPC_URLS="https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY"
export CHAIN_ID=1
export START_BLOCK=18000000

# 部署服务
make deploy-service
```

## 配置优先级

配置加载遵循以下优先级：
1. 环境变量（最高优先级）
2. 配置文件
3. 默认值

## 安全注意事项

- 演示模式使用的私钥是公开的Anvil测试密钥，不得用于生产环境
- 生产部署时必须使用自己的数据库凭证和RPC端点
- 所有敏感信息都应通过环境变量传入，而非硬编码在源码中