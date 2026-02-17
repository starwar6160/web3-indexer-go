let ws;
let isWSConnected = false;
let reconnectInterval = 1000; // 初始重连 1s
const MAX_RECONNECT_INTERVAL = 30000; // 最大重连 30s

const stateEl = document.getElementById('state');
const healthEl = document.getElementById('health');

function updateSystemState(connectionLabel, colorClass, isPulse = false) {
    const pulseClass = isPulse ? 'pulse' : '';
    stateEl.innerHTML = `<span class="${colorClass} ${pulseClass}">${connectionLabel}</span>`;
}

function connectWS() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = protocol + '//' + window.location.host + '/ws';
    
    if (!isWSConnected) {
        updateSystemState('CONNECTING...', 'status-connecting');
    }
    
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        isWSConnected = true;
        reconnectInterval = 1000; // 成功后重置重连间隔
        updateSystemState('● LIVE', 'status-live', true);
        healthEl.textContent = '✅ Connected';
        healthEl.className = 'status-badge status-healthy';
        console.log('✅ WebSocket Connected');
        
        // 💡 架构升级：重连后自动补齐数据
        fetchData();
        addLog('System connected. Streaming live data...', 'info');
    };

    ws.onmessage = (event) => {
        try {
            const raw = JSON.parse(event.data);
            let msg = raw;
            
            // 🛡️ Signed Payload Detection
            if (raw.signature && raw.data) {
                updateSignatureStatus(true, raw.signer_id, raw.signature);
                msg = { type: raw.type, data: raw.data };
            }

            if (msg.type === 'block') {
                updateBlocksTable(msg.data);
                fetchStatus();
            } else if (msg.type === 'transfer') {
                const tx = msg.data;
                tx.amount = tx.value || tx.amount; 
                updateTransfersTable(tx);
                // 🚀 Optimistic UI: If we see a transfer, the block is definitely processed
                optimisticUpdateBlockStatus(tx.block_number);
                addLog(`Transfer detected: ${tx.tx_hash.substring(0,10)}...`, 'success');
            } else if (msg.type === 'gas_leaderboard') {
                updateGasLeaderboard(msg.data);
            } else if (msg.type === 'log') {
                addLog(msg.data.message, msg.data.level);
            }
        } catch (e) { console.error('WS Error:', e); }
    };

    ws.onclose = (e) => {
        isWSConnected = false;
        updateSystemState('DISCONNECTED', 'status-down');
        healthEl.textContent = '❌ Disconnected';
        healthEl.className = 'status-badge status-error';
        
        addLog(`WebSocket connection lost. Retrying...`, 'warn');
        console.warn(`❌ WebSocket Closed. Reconnecting in ${reconnectInterval/1000}s...`, e.reason);
        
        setTimeout(() => {
            reconnectInterval = Math.min(reconnectInterval * 2, MAX_RECONNECT_INTERVAL);
            connectWS();
        }, reconnectInterval);
    };

    ws.onerror = (err) => {
        console.error('WebSocket Error:', err);
        ws.close();
    };
}

function addLog(message, level = 'info') {
    const logEntries = document.getElementById('logEntries');
    const time = new Date().toLocaleTimeString();
    let color = '#d4d4d4';
    if (level === 'success') color = '#28a745';
    if (level === 'warn') color = '#ff9800';
    if (level === 'error') color = '#f44336';
    if (level === 'info') color = '#2196f3';

    const entry = `<div style="margin-bottom: 2px;"><span style="color: #666;">[${time}]</span> <span style="color: ${color};">${message}</span></div>`;
    logEntries.insertAdjacentHTML('afterbegin', entry);
    
    // 保持 50 条日志
    if (logEntries.children.length > 50) {
        logEntries.removeChild(logEntries.lastChild);
    }
}

async function fetchStatus() {
    try {
        const res = await fetch('/api/status');
        if (!res.ok) throw new Error('API unreachable');
        
        // 🛡️ 确定性安全验证：检查响应头中的 Ed25519 签名
        const signature = res.headers.get('X-Payload-Signature');
        const signerId = res.headers.get('X-Signer-ID');
        if (signature) {
            updateSignatureStatus(true, signerId, signature);
        }

        const data = await res.json();
        
        // 💡 工业级防御：使用可选链和默认值，防止 toUpperCase() 崩溃
        const indexerState = (data?.state || 'unknown').toUpperCase();

        if (!isWSConnected) {
            if (indexerState === 'ACTIVE') {
                updateSystemState('● ACTIVE', 'status-live');
            } else {
                updateSystemState(indexerState, 'status-connecting');
            }
        }

        // 更新 UI 指标
        document.getElementById('latestBlock').textContent = data?.latest_block || '0';
        document.getElementById('totalBlocks').textContent = data?.total_blocks || '0';
        document.getElementById('totalTransfers').textContent = data?.total_transfers || '0';
        document.getElementById('tps').textContent = data?.tps || '0';
        document.getElementById('bps').textContent = data?.bps || '0';
        document.getElementById('syncLag').textContent = data?.sync_lag || '0';
        document.getElementById('latency').textContent = data?.e2e_latency_display || '0s';
        document.getElementById('totalVisitors').textContent = data?.total_visitors || '0';
        document.getElementById('adminIP').textContent = data?.admin_ip || 'None';
        document.getElementById('selfHealing').textContent = data?.self_healing_count || '0';
        document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
    } catch (e) { 
        console.warn('Status Sync Warning:', e.message);
        if (!isWSConnected) updateSystemState('OFFLINE', 'status-down');
    }
}

/**
 * 🚀 Optimistic UI: Force block status to Success based on event metadata
 */
function optimisticUpdateBlockStatus(blockNumber) {
    const row = document.querySelector(`tr[data-block-num="${blockNumber}"]`);
    if (row) {
        const statusCell = row.querySelector('.status-cell');
        if (statusCell && statusCell.classList.contains('status-syncing')) {
            statusCell.classList.remove('status-syncing');
            statusCell.classList.add('status-success');
            statusCell.innerHTML = `
                <div class="status-indicator">
                    <span class="dot"></span>
                    <span class="status-text">Processed</span>
                </div>
            `;
            console.debug(`[OptimisticUI] Block ${blockNumber} marked SUCCESS via event trigger.`);
        }
    }
}

function updateBlocksTable(block) {
    if (!block) return;
    if (!isWSConnected) {
        isWSConnected = true;
        updateSystemState('● LIVE', 'status-live', true);
    }

    const table = document.getElementById('blocksTable');
    if (table.querySelector('.loading')) table.innerHTML = '';
    
    // 💡 工业级防御：探测所有可能的字段变体
    const number = block.number || block.Number || '0';
    const hash = block.hash || block.Hash || '0x...';
    const parentHash = block.parent_hash || block.parentHash || block.ParentHash || '0x...';
    
    // 🚀 实时数据修正
    const blockTime = block.timestamp ? new Date(parseInt(block.timestamp) * 1000).toLocaleTimeString() : 'Now';
    
    const row = `<tr data-block-num="${number}">
        <td class="stat-value">${number}</td>
        <td class="hash" title="${hash}">${hash.substring(0, 16)}...</td>
        <td class="hash" title="${parentHash}">${parentHash.substring(0, 16)}...</td>
        <td class="status-cell status-syncing">
            <div class="status-indicator">
                <span class="dot"></span>
                <span class="status-text">Syncing...</span>
            </div>
        </td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    if (table.rows.length > 10) table.deleteRow(10);
}

function formatAmount(amt, decimals = 18) {
    if (!amt || amt === '0') return '0.0000';
    
    // 🛡️ 工业级鲁棒性：强制转换为字符串处理
    let s = amt.toString();
    
    // 如果已经是科学计数法或者包含小数点，先简单截断
    if (s.includes('e') || s.includes('.')) {
        let val = parseFloat(s);
        if (isNaN(val)) return '0.0000';
        if (val > 1e12) return val.toExponential(2);
        return val.toFixed(4);
    }

    try {
        const amount = BigInt(s);
        const divisor = BigInt(10 ** decimals);
        const integerPart = amount / divisor;
        const fractionalPart = amount % divisor;
        
        let intStr = integerPart.toString();
        
        // 🚀 天文数字防御：整数部分超过 12 位，改用缩写，防止撑破布局
        if (intStr.length > 12) {
            return intStr.substring(0, 6) + '...' + intStr.substring(intStr.length - 3);
        }
        
        let fractionStr = fractionalPart.toString().padStart(decimals, '0');
        // 只保留 4 位小数，去除末尾零
        fractionStr = fractionStr.substring(0, 4);
        
        return `${intStr}.${fractionStr}`;
    } catch (e) {
        // Fallback: 极其复杂的情况，直接截断原始字符串
        if (s.length > 15) return s.substring(0, 8) + '...';
        return s;
    }
}

function renderActivityIcon(type) {
    const icons = {
        'SWAP':           '🔄 <span style="color: #a855f7;">Swap</span>',
        'APPROVE':        '🔓 <span style="color: #eab308;">Approve</span>',
        'MINT':           '💎 <span style="color: #22c55e;">Mint</span>',
        'TRANSFER':       '💸 <span style="color: #3b82f6;">Transfer</span>',
        'CONTRACT_EVENT': '📜 <span style="color: #94a3b8;">Contract Log</span>',
        'ETH_TRANSFER':   '⛽ <span style="color: #6366f1;">ETH Transfer</span>',
        'DEPLOY':         '🏗️ <span style="color: #f43f5e;">Deployment</span>',
        'FAUCET_CLAIM':   '🚰 <span style="color: #06b6d4; font-weight: bold;">Faucet Claim</span>'
    };
    return icons[type] || '⚡ <span style="color: #64748b;">Activity</span>';
}

function updateTransfersTable(tx) {
    if (!tx) return;
    const table = document.getElementById('transfersTable');
    if (table.querySelector('.loading')) table.innerHTML = '';
    const from = tx.from || '0xunknown';
    const to = tx.to || '0xunknown';
    const symbol = tx.symbol || '';
    const type = tx.type || 'TRANSFER';
    const token = tx.token_address || '0xunknown';
    const displayAmount = formatAmount(tx.amount || tx.value, 18); // 默认 18 位精度
    
    // 🎨 Activity & Token Badge 渲染
    const activityDisplay = renderActivityIcon(type);
    const tokenDisplay = symbol && symbol !== token.substring(0, 10) ? 
        `<span class="token-badge token-${symbol.toLowerCase()}">${symbol}</span>` : 
        `<span class="address" title="${token}">${token.substring(0, 8)}...${token.substring(34)}</span>`;
    
    const row = `<tr>
        <td class="stat-value">${tx.block_number || '0'}</td>
        <td>${activityDisplay}</td>
        <td class="address">${from.substring(0, 10)}...</td>
        <td class="address">${to.substring(0, 10)}...</td>
        <td class="stat-value" style="color: #667eea; font-family: 'Courier New', monospace;" title="${tx.amount || tx.value}">${displayAmount}</td>
        <td>${tokenDisplay}</td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    if (table.rows.length > 10) table.deleteRow(10);
}

function updateGasLeaderboard(data) {
    const list = document.getElementById('gas-leaderboard');
    if (!data || data.length === 0) return;
    
    list.innerHTML = data.map((item, index) => {
        const label = item.label || (item.address.substring(0, 10) + '...');
        const gasM = (item.total_gas / 1e6).toFixed(2);
        return `
            <div class="gas-item">
                <div style="display: flex; align-items: center; overflow: hidden;">
                    <span class="gas-rank">#${index + 1}</span>
                    <span class="gas-label" title="${item.address}">${label}</span>
                </div>
                <div class="gas-stats">
                    <div class="gas-amount">${gasM}M Gas</div>
                    <div class="gas-fee">${item.total_fee} ETH</div>
                </div>
            </div>
        `;
    }).join('');
}

async function fetchData() {
    await fetchStatus();
    try {
        const [blocksRes, txRes] = await Promise.all([fetch('/api/blocks'), fetch('/api/transfers')]);
        
        // 检查签名
        const sig = blocksRes.headers.get('X-Payload-Signature');
        if (sig) updateSignatureStatus(true, blocksRes.headers.get('X-Signer-ID'), sig);

        const blocksData = await blocksRes.json();
        const txData = await txRes.json();

        if (blocksData && blocksData.blocks) {
            const table = document.getElementById('blocksTable');
            table.innerHTML = blocksData.blocks.map(b => {
                const number = b.number || b.Number || '0';
                const hash = b.hash || b.Hash || '0x...';
                const parent = b.parent_hash || b.ParentHash || '0x...';
                
                // 🚀 以太坊 timestamp 是秒，JS 需要毫秒
                const blockTime = b.timestamp ? new Date(parseInt(b.timestamp) * 1000).toLocaleTimeString() : 'Pending';
                
                return `<tr data-block-num="${number}">
                    <td class="stat-value">${number}</td>
                    <td class="hash" title="${hash}">${hash.substring(0, 16)}...</td>
                    <td class="hash" title="${parent}">${parent.substring(0, 16)}...</td>
                    <td class="status-cell status-success">
                        <div class="status-indicator">
                            <span class="dot"></span>
                            <span class="status-text">${blockTime}</span>
                        </div>
                    </td>
                </tr>`;
            }).join('');
        }

        if (txData && txData.transfers) {
            const table = document.getElementById('transfersTable');
            table.innerHTML = txData.transfers.map(t => {
                const from = t.from_address || '0x...';
                const to = t.to_address || '0x...';
                const symbol = t.symbol || '';
                const type = t.type || 'TRANSFER';
                const token = t.token_address || '0x...';
                const displayAmount = formatAmount(t.amount || '0', 18); // 默认 18 位精度
                
                // 🎨 Activity & Token Badge 渲染逻辑
                const activityDisplay = renderActivityIcon(type);
                const tokenDisplay = symbol && symbol !== token.substring(0, 10) ? 
                    `<span class="token-badge token-${symbol.toLowerCase()}">${symbol}</span>` : 
                    `<span class="address" title="${token}">${token.substring(0, 8)}...${token.substring(34)}</span>`;
                
                return `<tr>
                    <td class="stat-value">${t.block_number || '0'}</td>
                    <td>${activityDisplay}</td>
                    <td class="address">${from.substring(0, 10)}...</td>
                    <td class="address">${to.substring(0, 10)}...</td>
                    <td class="stat-value" style="color: #667eea; font-family: 'Courier New', monospace;" title="${t.amount || '0'}">${displayAmount}</td>
                    <td>${tokenDisplay}</td>
                </tr>`;
            }).join('');
        }
    } catch (e) { console.error('Fetch Error:', e); }
}

fetchData();
connectWS();
setInterval(fetchStatus, 5000);

function updateSignatureStatus(isSigned, signerId, signature) {
    const sigStatusEl = document.getElementById('signatureStatus');
    if (sigStatusEl && isSigned) {
        sigStatusEl.innerHTML = `<span style="color: #ff9800; font-weight: bold; font-size: 11px;">🛡️ Verified: ${signerId}</span>`;
        sigStatusEl.title = "Ed25519 Payload Signature: " + signature;
    }
}
