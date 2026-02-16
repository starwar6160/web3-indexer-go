#!/bin/bash
# 获取Anvil容器IP地址
ANVIL_IP=$(docker inspect web3-indexer-anvil | jq -r '.[0].NetworkSettings.Networks["web3-indexer-go_indexer-network"].IPAddress')
echo "http://$ANVIL_IP:8545"