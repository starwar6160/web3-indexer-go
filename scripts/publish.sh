#!/bin/bash

# ==============================================================================
# Web3 Indexer 工业级发布脚本
# ==============================================================================

set -e

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=== 启动 Web3 Indexer 生产级编译流程 ===${NC}"

# 1. 编译二进制文件 (启用静态链接)
echo -e "${YELLOW}Step 1: 正在编译二进制文件...${NC}"
mkdir -p bin
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/indexer ./cmd/indexer
echo -e "${GREEN}✅ 编译完成: bin/indexer (静态链接版)${NC}"

# 2. 生成 systemd 服务配置文件
echo -e "${YELLOW}Step 2: 生成 systemd 单元文件...${NC}"
PROJECT_ROOT=$(pwd)
SERVICE_FILE="web3-indexer.service"

cat > bin/$SERVICE_FILE <<EOF
[Unit]
Description=Web3 Indexer Go Service
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=$(whoami)
WorkingDirectory=$PROJECT_ROOT
# 启动前确保 Docker 基础设施已启动
ExecStartPre=/usr/bin/docker compose -f $PROJECT_ROOT/docker-compose.infra.yml up -d

# 关键环境变量
Environment=DATABASE_URL=postgres://postgres:postgres@localhost:15432/web3_indexer?sslmode=disable
Environment=RPC_URLS=http://localhost:8545
Environment=CHAIN_ID=31337
Environment=START_BLOCK=0
Environment=EMULATOR_ENABLED=true
Environment=EMULATOR_RPC_URL=http://localhost:8545
Environment=EMULATOR_PRIVATE_KEY=ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
Environment=EMULATOR_TX_INTERVAL=333ms
Environment=LOG_LEVEL=info
Environment=CONTINUOUS_MODE=true

ExecStart=$PROJECT_ROOT/bin/indexer
Restart=always
RestartSec=5
StandardOutput=append:$PROJECT_ROOT/bin/indexer.log
StandardError=append:$PROJECT_ROOT/bin/indexer.err.log

[Install]
WantedBy=multi-user.target
EOF

echo -e "${GREEN}✅ 服务文件已生成: bin/$SERVICE_FILE${NC}"

# 3. 提供部署指令
echo -e "\n${BLUE}=== 部署指南 ===${NC}"
echo -e "1. 部署服务: ${YELLOW}sudo cp bin/$SERVICE_FILE /etc/systemd/system/${NC}"
echo -e "2. 加载配置: ${YELLOW}sudo systemctl daemon-reload${NC}"
echo -e "3. 启动并启用: ${YELLOW}sudo systemctl enable --now web3-indexer${NC}"
echo -e "4. 查看日志: ${YELLOW}tail -f bin/indexer.log${NC}"
