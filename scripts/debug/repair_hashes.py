import requests
import psycopg2
from psycopg2.extras import RealDictCursor
import os
import time

# 配置
DB_URL = os.getenv("DATABASE_URL", "postgres://postgres:W3b3_Idx_Secur3_2026_Sec@localhost:15433/web3_sepolia?sslmode=disable")
RPC_URL = os.getenv("RPC_URL", "https://greatest-alpha-morning.ethereum-sepolia.quiknode.pro/acf2caf911f89ccdc17e965b59706700a8479bad/")

def get_rpc_block(number):
    payload = {
        "jsonrpc": "2.0",
        "method": "eth_getBlockByNumber",
        "params": [hex(number), False],
        "id": 1
    }
    resp = requests.post(RPC_URL, json=payload).json()
    return resp.get('result')

def repair():
    conn = psycopg2.connect(DB_URL)
    cur = conn.cursor(cursor_factory=RealDictCursor)
    
    print("🔍 Searching for broken hash chains...")
    cur.execute("SELECT number, hash FROM blocks WHERE parent_hash = '0x0000000000000000000000000000000000000000000000000000000000000000' OR parent_hash = '' ORDER BY number DESC")
    broken_blocks = cur.fetchall()
    
    if not broken_blocks:
        print("✅ All hash chains look healthy!")
        return

    print(f"🛠️ Found {len(broken_blocks)} broken blocks. Starting repair...")
    
    for block in broken_blocks:
        num = block['number']
        print(f"  -> Fixing block #{num} ...", end="", flush=True)
        
        rpc_data = get_rpc_block(num)
        if rpc_data:
            parent_hash = rpc_data.get('parentHash')
            if parent_hash:
                cur.execute("UPDATE blocks SET parent_hash = %s WHERE number = %s", (parent_hash, num))
                conn.commit()
                print(f" FIXED (Parent: {parent_hash[:10]}...)")
            else:
                print(" FAILED (No parentHash in RPC)")
        else:
            print(" FAILED (RPC error)")
        
        time.sleep(0.5) # 限流保护

    cur.close()
    conn.close()
    print("🎉 Repair session completed.")

if __name__ == "__main__":
    repair()
