let ws;
let isWSConnected = false;
const stateEl = document.getElementById('state');
const healthEl = document.getElementById('health');

function updateSystemState(connectionLabel, colorClass, isPulse = false) {
    const pulseClass = isPulse ? 'pulse' : '';
    stateEl.innerHTML = `<span class="${colorClass} ${pulseClass}">${connectionLabel}</span>`;
}

function connectWS() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = protocol + '//' + window.location.host + '/ws';
    
    updateSystemState('CONNECTING...', 'status-connecting');
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        isWSConnected = true;
        updateSystemState('● LIVE', 'status-live', true);
        healthEl.textContent = '✅ Healthy';
        healthEl.className = 'status-badge status-healthy';
        console.log('✅ WebSocket Connected');
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
            }
        } catch (e) { console.error('WS Error:', e); }
    };

    ws.onclose = () => {
        isWSConnected = false;
        updateSystemState('DISCONNECTED', 'status-down');
        console.log('❌ WebSocket Disconnected. Retrying in 5s...');
        setTimeout(connectWS, 5000);
    };

    ws.onerror = (err) => {
        console.error('WebSocket Error:', err);
    };
}

async function fetchStatus() {
    try {
        const res = await fetch('/api/status');
        if (!res.ok) throw new Error('API unreachable');
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
        document.getElementById('syncLag').textContent = data?.sync_lag || '0';
        document.getElementById('selfHealing').textContent = data?.self_healing_count || '0';
        document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
    } catch (e) { 
        console.warn('Status Sync Warning:', e.message);
        if (!isWSConnected) updateSystemState('OFFLINE', 'status-down');
    }
}

function updateBlocksTable(block) {
    if (!block) return;
    // 💡 只要收到区块，说明链路必通，强制点绿
    if (!isWSConnected) {
        isWSConnected = true;
        updateSystemState('● LIVE', 'status-live', true);
    }

    const table = document.getElementById('blocksTable');
    if (table.querySelector('.loading')) table.innerHTML = '';
    const row = `<tr>
        <td class="stat-value">${block.number}</td>
        <td class="hash">${block.hash.substring(0, 16)}...</td>
        <td class="hash">${block.hash.substring(0, 16)}...</td>
        <td>${new Date().toLocaleTimeString()}</td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    if (table.rows.length > 10) table.deleteRow(10);
}

function updateTransfersTable(tx) {
    const table = document.getElementById('transfersTable');
    if (table.querySelector('.loading')) table.innerHTML = '';
    const row = `<tr>
        <td class="stat-value">${tx.block_number}</td>
        <td class="address">${tx.from.substring(0, 10)}...</td>
        <td class="address">${tx.to.substring(0, 10)}...</td>
        <td class="stat-value" style="color: #667eea;">${tx.amount || tx.value}</td>
        <td class="address">${tx.token_address.substring(0, 10)}...</td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    if (table.rows.length > 10) table.deleteRow(10);
}

async function fetchData() {
    await fetchStatus();
    try {
        const [blocksRes, txRes] = await Promise.all([fetch('/api/blocks'), fetch('/api/transfers')]);
        const blocksData = await blocksRes.json();
        const txData = await txRes.json();

        if (blocksData.blocks) {
            const table = document.getElementById('blocksTable');
            table.innerHTML = blocksData.blocks.map(b => `<tr><td class="stat-value">${b.number}</td><td class="hash">${b.hash.substring(0, 16)}...</td><td class="hash">${b.parent_hash.substring(0, 16)}...</td><td>${new Date(b.processed_at).toLocaleString()}</td></tr>`).join('');
        }

        if (txData.transfers) {
            const table = document.getElementById('transfersTable');
            table.innerHTML = txData.transfers.map(t => `<tr><td class="stat-value">${t.block_number}</td><td class="address">${t.from_address.substring(0, 10)}...</td><td class="address">${t.to_address.substring(0, 10)}...</td><td class="stat-value">${t.amount}</td><td class="address">${t.token_address.substring(0, 10)}...</td></tr>`).join('');
        }
    } catch (e) { console.error('Fetch Error:', e); }
}

fetchData();
connectWS();
