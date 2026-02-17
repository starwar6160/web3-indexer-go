-- Anvil 模式：直接插入 Synthetic Transfers 用于验证网页逻辑
-- 用法: docker exec -it web3-indexer-db psql -U postgres -d web3_demo -f scripts/inject-mock-transfers.sql

-- 插入 10 笔模拟的 Transfer 事件（最近 10 个区块）
INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
VALUES
(60390, '0x' || encode(sha256('60390' || 'ANVIL_MOCK_1'), 'hex'), 99999, '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266', '0x70997970c51812dc3a010c7d01b50e0d17dc79ee', '1000000000000000000', '0x0000000000000000000000000000000000000000'),
(60389, '0x' || encode(sha256('60389' || 'ANVIL_MOCK_2'), 'hex'), 99999, '0x70997970c51812dc3a010c7d01b50e0d17dc79ee', '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc', '2000000000000000000', '0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48'),
(60388, '0x' || encode(sha256('60388' || 'ANVIL_MOCK_3'), 'hex'), 99999, '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc', '0x90f79bf6eb2c4f870365e785982e1f101e93b906', '3000000000000000000', '0xdac17f958d2ee523a2206206994597c13d831ec7'),
(60387, '0x' || encode(sha256('60387' || 'ANVIL_MOCK_4'), 'hex'), 99999, '0x90f79bf6eb2c4f870365e785982e1f101e93b906', '0x15d34aaf54267db7d7c367839aaf71a00a2c6a65', '4000000000000000000', '0x2260fac5e5542a773aa44fbcfedf7c193bc2c599'),
(60386, '0x' || encode(sha256('60386' || 'ANVIL_MOCK_5'), 'hex'), 99999, '0x15d34aaf54267db7d7c367839aaf71a00a2c6a65', '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266', '5000000000000000000', '0x0000000000000000000000000000000000000000'),
(60385, '0x' || encode(sha256('60385' || 'ANVIL_MOCK_6'), 'hex'), 99999, '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266', '0x70997970c51812dc3a010c7d01b50e0d17dc79ee', '6000000000000000000', '0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48'),
(60384, '0x' || encode(sha256('60384' || 'ANVIL_MOCK_7'), 'hex'), 99999, '0x70997970c51812dc3a010c7d01b50e0d17dc79ee', '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc', '7000000000000000000', '0xdac17f958d2ee523a2206206994597c13d831ec7'),
(60383, '0x' || encode(sha256('60383' || 'ANVIL_MOCK_8'), 'hex'), 99999, '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc', '0x90f79bf6eb2c4f870365e785982e1f101e93b906', '8000000000000000000', '0x2260fac5e5542a773aa44fbcfedf7c193bc2c599'),
(60382, '0x' || encode(sha256('60382' || 'ANVIL_MOCK_9'), 'hex'), 99999, '0x90f79bf6eb2c4f870365e785982e1f101e93b906', '0x15d34aaf54267db7d7c367839aaf71a00a2c6a65', '9000000000000000000', '0x0000000000000000000000000000000000000000'),
(60381, '0x' || encode(sha256('60381' || 'ANVIL_MOCK_10'), 'hex'), 99999, '0x15d34aaf54267db7d7c367839aaf71a00a2c6a65', '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266', '10000000000000000000', '0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48')
ON CONFLICT (block_number, log_index) DO NOTHING;

-- 验证插入结果
SELECT block_number, substring(from_address, 1, 20) || '...' as from_addr,
       substring(to_address, 1, 20) || '...' as to_addr,
       substring(token_address, 1, 20) || '...' as token,
       amount
FROM transfers
ORDER BY block_number DESC
LIMIT 10;
