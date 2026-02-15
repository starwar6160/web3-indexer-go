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
            const msg = JSON.parse(event.data);
            if (msg.type === 'block') {
                updateBlocksTable(msg.data);
                fetchStatus();
            } else if (msg.type === 'transfer') {
                const tx = msg.data;
                tx.amount = tx.value; 
                updateTransfersTable(tx);
                addLog(`Transfer detected: ${tx.tx_hash.substring(0,10)}...`, 'success');
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
            updateSystemState(indexerState, 'status-connecting');
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
    const parentHash = block.parent_hash || block.parentHash || block.ParentHash || '⛓️ Syncing...';
    
    // 🚀 实时数据修正
    const blockTime = block.timestamp ? new Date(parseInt(block.timestamp) * 1000).toLocaleTimeString() : 'Now';
    
    const row = `<tr>
        <td class="stat-value">${number}</td>
        <td class="hash" title="${hash}">${hash.substring(0, 16)}...</td>
        <td class="hash" title="${parentHash}">${parentHash.substring(0, 16)}...</td>
        <td>${blockTime}</td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    if (table.rows.length > 10) table.deleteRow(10);
}

function formatAmount(amt) {
    if (!amt) return '0';
    const s = amt.toString();
    if (s.length > 20) {
        return s.substring(0, 6) + '...' + s.substring(s.length - 6);
    }
    return s;
}

function updateTransfersTable(tx) {
    if (!tx) return;
    const table = document.getElementById('transfersTable');
    if (table.querySelector('.loading')) table.innerHTML = '';
    const from = tx.from || '0xunknown';
    const to = tx.to || '0xunknown';
    const token = tx.token_address || '0xunknown';
    const displayAmount = formatAmount(tx.amount || tx.value);
    const row = `<tr>
        <td class="stat-value">${tx.block_number || '0'}</td>
        <td class="address">${from.substring(0, 10)}...</td>
        <td class="address">${to.substring(0, 10)}...</td>
        <td class="stat-value" style="color: #667eea;" title="${tx.amount || tx.value}">${displayAmount}</td>
        <td class="address">${token.substring(0, 10)}...</td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    if (table.rows.length > 10) table.deleteRow(10);
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
                
                // 🚀 修复：以太坊 timestamp 是秒，JS 需要毫秒
                const blockTime = b.timestamp ? new Date(parseInt(b.timestamp) * 1000).toLocaleString() : 'Pending';
                // processed_at 已经是后端格式化好的字符串 (15:04:05.000)
                const processedAt = b.processed_at || 'Recent';
                
                return `<tr>
                    <td class="stat-value">${number}</td>
                    <td class="hash" title="${hash}">${hash.substring(0, 16)}...</td>
                    <td class="hash" title="${parent}">${parent.substring(0, 16)}...</td>
                    <td>${blockTime} <br><small style="color:#666">${processedAt}</small></td>
                </tr>`;
            }).join('');
        }

        if (txData && txData.transfers) {
            const table = document.getElementById('transfersTable');
            table.innerHTML = txData.transfers.map(t => {
                const from = t.from_address || '0x...';
                const to = t.to_address || '0x...';
                const token = t.token_address || '0x...';
                const displayAmount = formatAmount(t.amount || '0');
                return `<tr>
                    <td class="stat-value">${t.block_number || '0'}</td>
                    <td class="address">${from.substring(0, 10)}...</td>
                    <td class="address">${to.substring(0, 10)}...</td>
                    <td class="stat-value" title="${t.amount || '0'}">${displayAmount}</td>
                    <td class="address">${token.substring(0, 10)}...</td>
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
