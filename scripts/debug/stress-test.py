import asyncio
from web3 import Web3, AsyncWeb3
from web3.eth import AsyncEth
import time
import random

# ==============================================================================
# Web3 Indexer 工业级高频压测工具
# ==============================================================================

RPC_URL = "http://127.0.0.1:8545"
# Anvil 默认私钥之一
PRIVATE_KEY = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
SENDER = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
RECEIVER = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"

async def send_tx(w3, account, nonce):
    tx = {
        'nonce': nonce,
        'to': RECEIVER,
        'value': w3.to_wei(0.0001, 'ether'),
        'gas': 21000,
        'gasPrice': w3.to_wei(50, 'gwei'),
        'chainId': 31337
    }
    signed_tx = account.sign_transaction(tx)
    try:
        tx_hash = await w3.eth.send_raw_transaction(signed_tx.raw_transaction)
        return tx_hash
    except Exception as e:
        print(f"Error: {e}")
        return None

async def main():
    w3 = AsyncWeb3(AsyncWeb3.AsyncHTTPProvider(RPC_URL))
    account = w3.eth.account.from_key(PRIVATE_KEY)
    
    print(f"🚀 Starting Stress Test on {RPC_URL}...")
    print(f"Target TPS: ~100-200")

    start_time = time.time()
    tx_count = 0
    
    # 获取初始 nonce
    nonce = await w3.eth.get_transaction_count(SENDER)

    while True:
        # 批量发送以提高 TPS
        batch_size = 50
        tasks = []
        for i in range(batch_size):
            tasks.append(send_tx(w3, account, nonce + tx_count))
            tx_count += 1
        
        await asyncio.gather(*tasks)
        
        elapsed = time.time() - start_time
        current_tps = tx_count / elapsed
        print(f"Sent {tx_count} transactions... Current Avg TPS: {current_tps:.2f}")
        
        # 稍微控制一下节奏，防止 Anvil 崩溃
        await asyncio.sleep(0.1)

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("
Test stopped.")
