#!/bin/bash

# ==============================================================================
# Web3 Indexer 生产级一键部署脚本
# 使用方法: sudo ./scripts/deploy.sh
# ==============================================================================

set -e

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

PROJECT_ROOT="/home/ubuntu/zwCode/web3-indexer-go"
SERVICE_NAME="web3-indexer"
BINARY_NAME="indexer"

echo -e "${BLUE}=== 启动工业级部署流水线 ===${NC}"

# 1. 权限检查
if [ "$EUID" -ne 0 ]; then 
  echo -e "${RED}错误: 请使用 sudo 运行此脚本${NC}"
  exit 1
fi

# 2. 进入项目根目录
cd $PROJECT_ROOT

# 3. 生产级编译 (静态链接 + 移除调试符号)
echo -e "${YELLOW}Step 1: 正在进行生产级增量编译...${NC}"
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$BINARY_NAME ./cmd/indexer
echo -e "${GREEN}✅ 编译成功: bin/$BINARY_NAME${NC}"

# 4. 更新 Systemd 配置文件
echo -e "${YELLOW}Step 2: 同步 Systemd 单元文件...${NC}"
if [ -f "bin/$SERVICE_NAME.service" ]; then
    cp bin/$SERVICE_NAME.service /etc/systemd/system/
    echo -e "${GREEN}✅ 服务文件已同步至 /etc/systemd/system/${NC}"
else
    echo -e "${RED}警告: 未发现 bin/$SERVICE_NAME.service，将跳过配置更新${NC}"
fi

# 5. 重载并重启服务
echo -e "${YELLOW}Step 3: 重载配置并重启服务 [Graceful Restart]...${NC}"
systemctl daemon-reload
systemctl enable $SERVICE_NAME.service
systemctl restart $SERVICE_NAME.service

# 6. 状态验证
echo -e "${YELLOW}Step 4: 正在执行健康检查...${NC}"
sleep 2
SERVICE_STATUS=$(systemctl is-active $SERVICE_NAME)

if [ "$SERVICE_STATUS" = "active" ]; then
    echo -e "${GREEN}🚀 部署圆满成功！${NC}"
    echo -e "服务当前状态: ${GREEN}RUNNING${NC}"
    echo -e "实时日志查看: ${BLUE}journalctl -u $SERVICE_NAME -f${NC}"
    echo -e "Dashboard 地址: ${BLUE}https://demo2.st6160.click${NC}"
else
    echo -e "${RED}❌ 部署失败！请执行 'journalctl -u $SERVICE_NAME -n 50' 查看原因${NC}"
    exit 1
fi
