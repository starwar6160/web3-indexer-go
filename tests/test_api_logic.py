import requests
import pytest
import time
import os

# 配置：根据环境自动切换 API 地址
BASE_URL = os.getenv("INDEXER_API_URL", "http://localhost:8081/api")

@pytest.fixture(scope="session", autouse=True)
def warm_up():
    """测试前的预热：唤醒懒惰索引器"""
    print(f"\n[Warm-up] Poking indexer at {BASE_URL}/status ...")
    try:
        # 第一次点击触发
        requests.get(f"{BASE_URL}/status", timeout=5)
        # 给索引器几秒钟开始抓取数据并写入 DB
        print("[Warm-up] Waiting 5s for first block to be indexed...")
        time.sleep(5)
    except Exception as e:
        print(f"Warning: Could not connect to Indexer API at {BASE_URL}: {e}")

def test_status_logic_guards():
    """
    逻辑守卫 1: 检查同步高度与链头高度的业务合理性
    """
    resp = requests.get(f"{BASE_URL}/status")
    assert resp.status_code == 200
    data = resp.json()
    
    latest_on_chain = int(data['latest_block'])
    latest_indexed = int(data['latest_indexed'])
    sync_lag = data['sync_lag']
    
    # 守卫：同步高度永远不应超过链头高度
    assert latest_indexed <= latest_on_chain, f"🔥 数据越界！已同步({latest_indexed}) > 链头({latest_on_chain})"
    
    # 守卫：Lag 计算必须一致 (链头 - 同步 = Lag)
    calculated_lag = latest_on_chain - latest_indexed
    # 允许 10 个块的误差，考虑到测试网同步延迟和并发更新
    assert abs(calculated_lag - sync_lag) <= 10, f"🔥 Lag 不一致！计算值为 {calculated_lag}, API 返回为 {sync_lag}"

def test_hash_chain_integrity():
    """
    逻辑守卫 2: 检查区块哈希链的完整性（防止哈希自指和断链）
    """
    resp = requests.get(f"{BASE_URL}/blocks")
    assert resp.status_code == 200
    blocks = resp.json().get('blocks', [])
    
    if not blocks:
        pytest.skip("No blocks indexed yet, skipping chain integrity test.")

    for i in range(len(blocks) - 1):
        curr = blocks[i]
        prev = blocks[i+1] # API ORDER BY number DESC
        
        curr_num = int(curr['number'])
        prev_num = int(prev['number'])
        
        # 1. 哈希自指检测
        assert curr['hash'] != curr['parent_hash'], f"🔥 发现哈希自指！Block #{curr_num} hash == parent_hash"
        
        # 2. 链式指向检测 (仅当块是连续的时候检查)
        if curr_num == prev_num + 1:
            assert curr['parent_hash'] == prev['hash'], f"🔥 哈希断链！#{curr_num} 的 parent_hash 与 #{prev_num} 的 hash 不匹配"
        else:
            print(f"\n[Info] Skipping hash chain check for non-consecutive blocks #{prev_num} and #{curr_num}")
        
        # 3. 连续性检测 (该项作为警告，因为 catch-up 期间可能有 Gap)
        # assert curr_num == prev_num + 1, f"🔥 区块号不连续！从 {prev_num} 跳到了 {curr_num}"

def test_lazy_indexer_state_logic():
    """
    逻辑守卫 3: 检查懒惰索引器的内部状态一致性
    """
    resp = requests.get(f"{BASE_URL}/status")
    data = resp.json()
    
    if 'lazy_indexer' in data:
        lazy = data['lazy_indexer']
        # 根据 LazyManager.GetStatus(), 字段是 'mode'
        if lazy.get('mode') == 'active':
            assert data['sync_lag'] >= 0
            print(f"\n[Info] Lazy Indexer is ACTIVE, catching up {data['sync_lag']} blocks.")
        elif lazy.get('mode') == 'lazy':
            print(f"\n[Info] Lazy Indexer is IDLE (Lazy Mode).")
        else:
            pytest.fail(f"Unknown lazy indexer mode: {lazy.get('mode')}")

def test_transfer_data_sanity():
    """
    逻辑守卫 4: 检查转账数据的基本字段合法性
    """
    resp = requests.get(f"{BASE_URL}/transfers")
    assert resp.status_code == 200
    transfers = resp.json().get('transfers', [])
    
    if not transfers:
        print("\n[Info] No transfers found yet, skipping sanity check.")
        return

    for tx in transfers:
        from_addr = tx['from_address'].strip()
        assert from_addr.startswith('0x')
        if len(from_addr) != 42:
            # Special label check (e.g. 0xcontract_creation)
            assert from_addr == '0xcontract_creation' or from_addr == '0x0'
        
        # Guard: Support 'multiple' or empty for generic contract events
        to_addr = tx['to_address'].strip()
        if to_addr and to_addr != 'multiple':
            assert to_addr.startswith('0x')
            if len(to_addr) != 42:
                # Special labels allowed here too
                assert to_addr == '0xcontract_creation' or to_addr == '0x0'
            
        assert tx['tx_hash'].strip().startswith('0x')
        assert len(tx['tx_hash'].strip()) == 66

def test_debug_snapshot_integrity():
    """
    逻辑守卫 5: 检查调试快照聚合接口的数据完整性
    """
    resp = requests.get(f"{BASE_URL}/debug/snapshot")
    assert resp.status_code == 200
    data = resp.json()
    
    # 1. 结构完整性
    assert 'engine_status' in data
    assert 'data_integrity' in data
    assert 'recent_data_samples' in data
    
    # 2. 引擎状态自洽
    engine = data['engine_status']
    assert 'mode' in engine
    assert 'reality_gap' in engine
    assert 'is_healthy' in engine
    
    # 3. 数据一致性校验
    integrity = data['data_integrity']
    assert integrity['latest_rpc_block'] >= integrity['latest_db_block'], \
        f"🔥 逻辑矛盾！RPC 高度({integrity['latest_rpc_block']}) < DB 高度({integrity['latest_db_block']})"
    
    # 4. 样本可用性
    samples = data['recent_data_samples']
    assert 'latest_blocks' in samples
    assert 'latest_txs' in samples
    print(f"\n[Info] Debug Snapshot validated: Gap={engine['reality_gap']}, RPC={integrity['latest_rpc_block']}")
