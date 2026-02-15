import requests
import pytest
import time
import os

# 配置：根据环境自动切换 API 地址
BASE_URL = os.getenv("INDEXER_API_URL", "http://localhost:8081/api")

@pytest.fixture(scope="session", autouse=True)
def warm_up():
    """测试前的预热：唤醒懒惰索引器"""
    print(f"
[Warm-up] Poking indexer at {BASE_URL}/status ...")
    try:
        requests.get(f"{BASE_URL}/status", timeout=5)
        # 给索引器一点时间开始抓取数据
        time.sleep(2)
    except Exception as e:
        pytest.fail(f"Could not connect to Indexer API at {BASE_URL}: {e}")

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
    # 考虑到并发请求可能有 1-2 块的漂移，允许小范围误差
    calculated_lag = latest_on_chain - latest_indexed
    assert abs(calculated_lag - sync_lag) <= 2, f"🔥 Lag 不一致！计算值为 {calculated_lag}, API 返回为 {sync_lag}"

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
        prev = blocks[i+1] # 注意：API 是 ORDER BY number DESC
        
        curr_num = int(curr['number'])
        prev_num = int(prev['number'])
        
        # 1. 哈希自指检测
        assert curr['hash'] != curr['parent_hash'], f"🔥 发现哈希自指！Block #{curr_num} hash == parent_hash"
        
        # 2. 链式指向检测 (当前块的 ParentHash 必须等于前一个块的 Hash)
        assert curr['parent_hash'] == prev['hash'], f"🔥 哈希断链！#{curr_num} 的 parent_hash 与 #{prev_num} 的 hash 不匹配"
        
        # 3. 连续性检测
        assert curr_num == prev_num + 1, f"🔥 区块号不连续！从 {prev_num} 跳到了 {curr_num}"

def test_lazy_indexer_state_logic():
    """
    逻辑守卫 3: 检查懒惰索引器的内部状态一致性
    """
    resp = requests.get(f"{BASE_URL}/status")
    data = resp.json()
    
    if 'lazy_indexer' in data:
        lazy = data['lazy_indexer']
        # 如果 is_active 为 true，则正在追赶
        if lazy['is_active']:
            assert data['sync_lag'] >= 0
            print(f"
[Info] Lazy Indexer is ACTIVE, catching up {data['sync_lag']} blocks.")
        else:
            print(f"
[Info] Lazy Indexer is IDLE.")

def test_transfer_data_sanity():
    """
    逻辑守卫 4: 检查转账数据的基本字段合法性
    """
    resp = requests.get(f"{BASE_URL}/transfers")
    assert resp.status_code == 200
    transfers = resp.json().get('transfers', [])
    
    for tx in transfers:
        # 地址必须是 0x 开头的 42 位字符串
        assert tx['from_address'].startswith('0x')
        assert len(tx['from_address']) == 42
        assert tx['to_address'].startswith('0x')
        assert len(tx['to_address']) == 42
        # TxHash 必须是 0x 开头的 66 位字符串
        assert tx['tx_hash'].startswith('0x')
        assert len(tx['tx_hash']) == 66
