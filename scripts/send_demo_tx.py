#!/usr/bin/env python3

import requests
import json
import time
import sys

RPC_URL = "http://localhost:8545"
FROM_ADDRESS = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
TO_ADDRESS = "0x70997970C51812e339D9B73b0245ad59e5E05a77"

def send_rpc(method, params):
    """Send JSON-RPC request to Anvil"""
    payload = {
        "jsonrpc": "2.0",
        "method": method,
        "params": params,
        "id": 1
    }
    try:
        response = requests.post(RPC_URL, json=payload, timeout=5)
        result = response.json()
        return result.get("result")
    except Exception as e:
        print(f"❌ RPC Error: {e}")
        return None

def main():
    print("🚀 Generating demo transactions on Anvil...")
    print(f"RPC URL: {RPC_URL}")
    print(f"From: {FROM_ADDRESS}")
    print(f"To: {TO_ADDRESS}")
    print()
    
    # Get current nonce
    nonce_hex = send_rpc("eth_getTransactionCount", [FROM_ADDRESS, "latest"])
    if not nonce_hex:
        print("❌ Failed to get nonce")
        return
    
    nonce = int(nonce_hex, 16)
    print(f"✅ Current nonce: {nonce}")
    print()
    
    # Send 5 ETH transfer transactions
    for i in range(5):
        current_nonce = nonce + i
        nonce_hex = hex(current_nonce)
        
        # Simple ETH transfer (0.1 ETH)
        value = hex(100000000000000000 + i * 10000000000000000)
        
        tx_params = {
            "from": FROM_ADDRESS,
            "to": TO_ADDRESS,
            "value": value,
            "gas": "0x5208",
            "gasPrice": "0x3b9aca00"  # 1 Gwei
        }
        
        tx_hash = send_rpc("eth_sendTransaction", [tx_params])
        
        if tx_hash:
            print(f"✅ TX {i+1} sent: {tx_hash}")
        else:
            print(f"❌ TX {i+1} failed")
        
        time.sleep(1)
    
    print()
    print("✨ Demo transactions complete!")
    print("📊 Check Dashboard at http://localhost:8080 to see the transactions")
    print("💾 Data should appear in the blocks and transfers tables")

if __name__ == "__main__":
    main()
