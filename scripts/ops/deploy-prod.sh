#!/bin/bash

# ==============================================================================
# Web3 Indexer 生产级一键部署脚本 (MiniPC/Server 专用)
# ==============================================================================

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=== 开始执行生产级部署流程 ===${NC}"

# 1. 编译最新的静态二进制文件
echo -e "${YELLOW}Step 1: 编译加固版二进制文件...${NC}"
./scripts/publish.sh

# 2. 部署 Systemd 服务
echo -e "${YELLOW}Step 2: 注册 Systemd 守护进程...${NC}"
SERVICE_FILE="web3-indexer.service"
sudo cp bin/$SERVICE_FILE /etc/systemd/system/

# 3. 重载并启动
echo -e "${YELLOW}Step 3: 启动服务并配置自启动...${NC}"
sudo systemctl daemon-reload
sudo systemctl enable web3-indexer
sudo systemctl restart web3-indexer

# 4. 检查状态
echo -e "${YELLOW}Step 4: 验证服务状态...${NC}"
sleep 2
if systemctl is-active --quiet web3-indexer; then
    echo -e "${GREEN}✅ 部署成功！索引器正在后台运行。${NC}"
else
    echo -e "${RED}❌ 部署失败，请检查 journalctl -u web3-indexer${NC}"
    exit 1
fi

echo -e "
${BLUE}=== 运维指令集 ===${NC}"
echo -e "查看日志: ${YELLOW}tail -f logs/indexer.log${NC}"
echo -e "重置环境: ${YELLOW}./scripts/clean-env.sh${NC}"
echo -e "停止服务: ${YELLOW}sudo systemctl stop web3-indexer${NC}"
echo -e "实时开发: ${YELLOW}air${NC}"
