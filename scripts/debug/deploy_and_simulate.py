#!/usr/bin/env python3
"""
ERC20 Contract Deployment and Traffic Simulation Engine

This script deploys a simple ERC20 contract to Anvil and continuously
generates transfer events to simulate real-time blockchain activity.

The contract emits Transfer events that the Go Indexer will capture
and display on the Dashboard in real-time.
"""

import time
import random
import sys
from web3 import Web3
from eth_account import Account

# Connect to local Anvil
w3 = Web3(Web3.HTTPProvider('http://localhost:8545'))

if not w3.is_connected():
    print("❌ Failed to connect to Anvil at http://localhost:8545")
    sys.exit(1)

print(f"✅ Connected to Anvil")
print(f"   Chain ID: {w3.eth.chain_id}")
print(f"   Latest Block: {w3.eth.block_number}")

# Anvil default accounts (pre-funded with 10000 ETH each)
accounts = w3.eth.accounts
deployer = accounts[0]
user1 = accounts[1]
user2 = accounts[2]
user3 = accounts[3]

print(f"\n👤 Deployer: {deployer}")
print(f"👤 User 1:   {user1}")
print(f"👤 User 2:   {user2}")
print(f"👤 User 3:   {user3}")

# Simple ERC20 contract bytecode (compiled SimpleToken with transfer function)
# This is a minimal ERC20 that supports transfer() and emits Transfer events
BYTECODE = "608060405234801561001057600080fd5b506040516103c43803806103c48339818101604052602081101561003357600080fd5b50516000805460ff19166001179055600180546001600160a01b0319163317905560028190556040516001600160a01b0390911690600090600080516020610374833981519152908290a3506102f0806100906000396000f3fe608060405234801561001057600080fd5b50600436106100415760003560e01c8063a9059cbb14610046578063dd62ed3e14610079578063313ce56714610099575b600080fd5b6100636004803603604081101561005c57600080fd5b506100b8565b604080519115158252519081900360200190f35b6100876004803603604081101561008f57600080fd5b506100d0565b60408051918252519081900360200190f35b6100a16100e7565b6040805160ff9092168252519081900360200190f35b60006100c53384846100eb565b50600192915050565b6001600160a01b0391821660009081526003602090815260408083209490951682529290925250205490565b601290565b6001600160a01b0383166101305760405162461bcd60e51b8152600401808060200182810382526024815260200180610350602491396040910194505050505060405180910390fd5b6001600160a01b0382166101755760405162461bcd60e51b815260040180806020018281038252602281526020018061032e602291396040910194505050505060405180910390fd5b6001600160a01b038084166000908152600360209081526040808320938616835292905220548181101561020d5760405162461bcd60e51b815260040180806020018281038252602681526020018061036a602691396040910194505050505060405180910390fd5b6001600160a01b038085166000818152600360209081526040808320948816835293905283902080548590039055600254600160a060020a03168686836040516001600160a01b038816929190600080516020610374833981519152908290a4600192505050939250505056fe4f6e6c7920746865206f776e65722063616e206d696e7420746f6b656e73a265627a7a72315820"

# Simplified ABI for Transfer event and transfer function
ABI = [
    {
        "anonymous": False,
        "inputs": [
            {"indexed": True, "name": "from", "type": "address"},
            {"indexed": True, "name": "to", "type": "address"},
            {"indexed": False, "name": "value", "type": "uint256"}
        ],
        "name": "Transfer",
        "type": "event"
    },
    {
        "constant": False,
        "inputs": [
            {"name": "_to", "type": "address"},
            {"name": "_value", "type": "uint256"}
        ],
        "name": "transfer",
        "outputs": [{"name": "", "type": "bool"}],
        "stateMutability": "nonpayable",
        "type": "function"
    }
]

# Even simpler bytecode for a minimal ERC20 that just emits Transfer events
# This is a contract that accepts transfer calls and emits Transfer events
SIMPLE_BYTECODE = "6080604052348015600f57600080fd5b50609f80601d6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c8063a9059cbb14602d575b600080fd5b6040516001600160a01b0360443516906024359060648051918152602001908051906020019060208101906040810160405261006992919061008b565b60405180910390f35b6001600160a01b03167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60405180910390a3600160405180910390f3"

def deploy_contract():
    """Deploy a simple ERC20 contract to Anvil"""
    print("\n🚀 Deploying ERC20 contract...")
    
    try:
        # Send deployment transaction
        tx_hash = w3.eth.send_transaction({
            "from": deployer,
            "data": SIMPLE_BYTECODE,
            "gas": 200000,
            "gasPrice": w3.to_wei(1, 'gwei')
        })
        
        # Wait for receipt
        tx_receipt = w3.eth.wait_for_transaction_receipt(tx_hash, timeout=30)
        contract_address = tx_receipt.contractAddress
        
        print(f"✅ Contract deployed successfully!")
        print(f"   Address: {contract_address}")
        print(f"   TX Hash: {tx_hash.hex()}")
        print(f"   Block: {tx_receipt.blockNumber}")
        
        return contract_address
    except Exception as e:
        print(f"❌ Deployment failed: {e}")
        sys.exit(1)

def simulate_transfers(contract_address):
    """Continuously simulate ERC20 transfer events"""
    print(f"\n🎨 Starting traffic simulation...")
    print(f"   - New block every 3 seconds")
    print(f"   - ERC20 transfer every 8 seconds")
    print(f"   - Contract: {contract_address}\n")
    
    contract = w3.eth.contract(address=contract_address, abi=ABI)
    
    last_block_time = time.time()
    last_tx_time = time.time()
    tx_count = 0
    
    try:
        while True:
            now = time.time()
            
            # Generate a new block every 3 seconds by sending a tiny ETH transfer
            if now - last_block_time >= 3:
                try:
                    # Send minimal ETH transfer to trigger block mining
                    w3.eth.send_transaction({
                        "from": accounts[9],
                        "to": accounts[8],
                        "value": w3.to_wei(0.0001, 'ether'),
                        "gas": 21000,
                        "gasPrice": w3.to_wei(1, 'gwei')
                    })
                    block_num = w3.eth.block_number
                    print(f"📦 Block #{block_num} mined at {time.strftime('%H:%M:%S')}")
                    last_block_time = now
                except Exception as e:
                    print(f"⚠️  Block generation failed: {e}")
            
            # Generate ERC20 transfer every 8 seconds
            if now - last_tx_time >= 8:
                try:
                    # Select random sender and receiver
                    sender = random.choice([deployer, user1, user2, user3])
                    receiver = random.choice([deployer, user1, user2, user3])
                    
                    # Ensure sender != receiver
                    while sender == receiver:
                        receiver = random.choice([deployer, user1, user2, user3])
                    
                    # Random amount (1-1000 tokens)
                    amount = random.randint(1, 1000)
                    
                    # Call transfer function (will emit Transfer event)
                    tx_hash = contract.functions.transfer(receiver, amount).transact({
                        "from": sender,
                        "gas": 100000,
                        "gasPrice": w3.to_wei(1, 'gwei')
                    })

                    # ✅ 关键修复：等待交易确认，确保物理现实存在
                    try:
                        receipt = w3.eth.wait_for_transaction_receipt(tx_hash, timeout=10)
                        tx_count += 1
                        print(f"💸 Transfer #{tx_count}: {amount} tokens from {sender[:8]}... to {receiver[:8]}...")
                        print(f"   TX: {tx_hash.hex()}")
                        print(f"   ✅ Confirmed in block {receipt.blockNumber}, logs: {len(receipt.logs)}")

                        # 验证 Transfer 事件确实被触发
                        if len(receipt.logs) > 0:
                            print(f"   🎯 Transfer event emitted!")
                        else:
                            print(f"   ⚠️  Warning: No logs found in receipt")

                        last_tx_time = now
                    except Exception as e:
                        print(f"❌ Transfer failed to confirm: {e}")
                        # 继续下一次循环，不更新 last_tx_time
                except Exception as e:
                    print(f"⚠️  Transfer failed: {e}")
            
            time.sleep(0.5)
    
    except KeyboardInterrupt:
        print(f"\n\n✋ Simulation stopped by user")
        print(f"   Total transfers: {tx_count}")
        print(f"   Final block: {w3.eth.block_number}")

def main():
    """Main entry point"""
    print("=" * 60)
    print("🌐 ERC20 Contract Deployment & Traffic Simulation Engine")
    print("=" * 60)
    
    # Deploy contract
    contract_address = deploy_contract()
    
    # Start simulation
    print(f"\n📊 Contract address for Indexer monitoring:")
    print(f"   {contract_address}")
    print(f"\n💡 Add this to your docker-compose.yml:")
    print(f"   WATCH_ADDRESSES={contract_address}")
    
    simulate_transfers(contract_address)

if __name__ == "__main__":
    main()
