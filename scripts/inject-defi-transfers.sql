-- 🏭 DeFi 模拟数据注入脚本
-- 模拟高频套利、Flashloan、MEV 等复杂场景
-- 用法: psql -h 127.0.0.1 -p 15432 -U postgres -d web3_demo -f scripts/inject-defi-transfers.sql

BEGIN;

-- 1. 普通 Swap 交易（60%）
INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
VALUES
-- Uniswap V3: USDC -> WETH
(60400, '0x' || encode(sha256('60400' || 'SWAP' || '1'), 'hex'), 0,
 '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '2847560000', -- 2847.56 USDC (6 decimals)
 '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48'),

-- Uniswap V3: WETH -> USDT
(60400, '0x' || encode(sha256('60400' || 'SWAP' || '2'), 'hex'), 1,
 '0x70997970c51812dc3a010c7d01b50e0d17dc79c8',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '50000000000000000000', -- 50 WETH (18 decimals)
 '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2'),

-- Curve: 3Pool USDT -> USDC
(60401, '0x' || encode(sha256('60401' || 'SWAP' || '3'), 'hex'), 0,
 '0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc',
 '0xbEbc44782C7dB0a1A60Cb6fe97d0b483032FF1C7',
 '15000000', -- 15 USDT (6 decimals)
 '0xdAC17F958D2ee523a2206206994597C13D831ec7'),

-- 小额零售交易
(60401, '0x' || encode(sha256('60401' || 'SWAP' || '4'), 'hex'), 1,
 '0x90f79bf6eb2c4f870365e785982e1f101e93b906',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '997000', -- 0.997 USDC
 '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48'),

-- 中额交易
(60402, '0x' || encode(sha256('60402' || 'SWAP' || '5'), 'hex'), 0,
 '0x15d34aaf54267db7d7c367839aaf71a00a2c6a65',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '450000000', -- 450 USDC
 '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48')

ON CONFLICT (block_number, log_index) DO NOTHING;


-- 2. 套利交易（20%）- 大额、快速
INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
VALUES
-- MEV Bot #1: WBTC 套利
(60403, '0x' || encode(sha256('60403' || 'ARBITRAGE' || '1'), 'hex'), 0,
 '0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '50000000', -- 0.5 WBTC (8 decimals) ≈ $22,500
 '0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599'),

-- MEV Bot #2: WETH 套利
(60403, '0x' || encode(sha256('60403' || 'ARBITRAGE' || '2'), 'hex'), 1,
 '0x5615dEb798BB3E4dFa01397d0Db2C6b0404A38D7',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '85000000000000000000', -- 85 WETH ≈ $255,000
 '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2'),

-- Binance Hot Wallet: 超大额套利
(60404, '0x' || encode(sha256('60404' || 'ARBITRAGE' || '3'), 'hex'), 0,
 '0x3f5CE5FBFe3E9af3971dD833D26bA9b5C936f0bE',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '50000000000', -- 50,000 USDC ≈ $50,000
 '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48')

ON CONFLICT (block_number, log_index) DO NOTHING;


-- 3. Flashloan（10%）- 超巨额
INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
VALUES
-- Aave V3: DAI Flashloan
(60405, '0x' || encode(sha256('60405' || 'FLASHLOAN' || '1'), 'hex'), 0,
 '0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2',
 '0xBA12222222228d8Ba445958a75a0704d566BF2C8',
 '500000000000000000000000', -- 500,000 DAI (18 decimals)
 '0x6B175474E89094C44Da98b954EedeAC495271d0F'),

-- Balancer: USDC Flashloan
(60405, '0x' || encode(sha256('60405' || 'FLASHLOAN' || '2'), 'hex'), 1,
 '0xBA12222222228d8Ba445958a75a0704d566BF2C8',
 '0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2',
 '75000000000', -- 75,000 USDC
 '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48'),

-- Uniswap V3 Flashloan
(60406, '0x' || encode(sha256('60406' || 'FLASHLOAN' || '3'), 'hex'), 0,
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '0xBA12222222228d8Ba445958a75a0704d566BF2C8',
 '120000000000000000000', -- 120 WETH
 '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2')

ON CONFLICT (block_number, log_index) DO NOTHING;


-- 4. MEV - Sandwich Attack（10%）
INSERT INTO transfers (block_number, tx_hash, log_index, from_address, to_address, amount, token_address)
VALUES
-- Frontrun: MEV Bot 抢跑
(60407, '0x' || encode(sha256('60407' || 'MEV' || 'FRONTRUN'), 'hex'), 0,
 '0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '15000000000000000000', -- 15 WETH
 '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2'),

-- Victim: 被夹击的用户交易
(60407, '0x' || encode(sha256('60407' || 'MEV' || 'VICTIM'), 'hex'), 1,
 '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '12000000000000000000', -- 12 WETH (滑点更大)
 '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2'),

-- Backrun: MEV Bot 跑单
(60407, '0x' || encode(sha256('60407' || 'MEV' || 'BACKRUN'), 'hex'), 2,
 '0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0',
 '0xE592427A0AEce92De3Edee1F18E0157C05861564',
 '18000000000000000000', -- 18 WETH (获利)
 '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2')

ON CONFLICT (block_number, log_index) DO NOTHING;


COMMIT;

-- 验证结果
SELECT
    block_number,
    CASE
        WHEN from_address LIKE '0x742d%' THEN '🦈 MEV Bot'
        WHEN from_address LIKE '0x8787%' THEN '⚡ Flashloan'
        WHEN from_address LIKE '0xE592%' THEN '🔄 Swap'
        WHEN to_address LIKE '0xE592%' THEN '🔄 Swap'
        ELSE '👤 User'
    END as tx_type,
    CASE
        WHEN token_address = '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48' THEN 'USDC'
        WHEN token_address = '0xdAC17F958D2ee523a2206206994597C13D831ec7' THEN 'USDT'
        WHEN token_address = '0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599' THEN 'WBTC'
        WHEN token_address = '0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2' THEN 'WETH'
        WHEN token_address = '0x6B175474E89094C44Da98b954EedeAC495271d0F' THEN 'DAI'
        ELSE 'Unknown'
    END as token,
    amount,
    substring(from_address, 1, 10) || '...' as from_addr
FROM transfers
WHERE block_number >= 60400
ORDER BY block_number, log_index
LIMIT 20;
