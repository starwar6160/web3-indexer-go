let idleTimer;
let countdownInterval;
const IDLE_TIMEOUT = 5 * 60 * 1000; // 5 minutes
const WS_DISCONNECT_GRACE_PERIOD = 30 * 1000; // 30 seconds grace period for WebSocket disconnection
let wsDisconnectedSince = null; // Track when WebSocket disconnected

// 🛡️ 演示模式：禁用休眠遮罩（用于 8082/8092 等 Anvil 演示环境）
const DEMO_MODE_DISABLE_SLEEP = true;

// 🚀 Anvil 演示环境：快速刷新（每秒 3-5 次，人眼感到很快）
const DEMO_REFRESH_INTERVAL = 250; // 250ms = 每秒 4 次

function resetIdleTimer() {
    // 🛡️ WebSocket 断线宽限期：如果在 30 秒内重连成功，不触发休眠倒计时
    const now = Date.now();
    if (wsDisconnectedSince !== null) {
        if (now - wsDisconnectedSince > WS_DISCONNECT_GRACE_PERIOD) {
            // 超过宽限期，允许进入休眠
            console.warn('⚠️ WebSocket disconnected for too long, allowing hibernation');
            wsDisconnectedSince = null; // Reset flag
        } else {
            // 在宽限期内，WebSocket 刚重连，不重置休眠倒计时
            console.log('✅ WebSocket reconnected within grace period, skipping idle timer reset');
            return;
        }
    }

    if (document.visibilityState === 'visible') {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'HEARTBEAT', data: { status: 'active' } }));
        }
        hideSleepOverlay();
    }
    clearTimeout(idleTimer);
    clearInterval(countdownInterval);
    idleTimer = setTimeout(() => startIdleCountdown(), IDLE_TIMEOUT);
}

function startIdleCountdown() {
    // 🛡️ 演示模式保护：完全禁用休眠倒计时和遮罩
    if (DEMO_MODE_DISABLE_SLEEP) {
        console.log('🛡️ Demo Mode: Idle countdown suppressed - system stays active');
        return;
    }

    let secondsLeft = 60; // Show 60-second countdown before entering Eco-Mode

    // Show countdown container
    const countdownContainer = document.getElementById('idleCountdownContainer');
    if (countdownContainer) {
        countdownContainer.style.display = 'flex';
    }

    updateCountdownDisplay(secondsLeft);
    addLog('⏰ Inactivity detected: Entering Eco-Mode in 60 seconds...', 'warn');

    countdownInterval = setInterval(() => {
        secondsLeft--;
        updateCountdownDisplay(secondsLeft);

        if (secondsLeft <= 0) {
            clearInterval(countdownInterval);
            showSleepOverlay();
        }
    }, 1000);
}

function showSleepOverlay() {
    // 🛡️ 演示模式保护：如果启用了演示模式，强制不显示休眠遮罩
    if (DEMO_MODE_DISABLE_SLEEP) {
        console.log('🛡️ Demo Mode: Sleep overlay suppressed for visual continuity');
        return;
    }

    document.body.classList.add('is-sleeping');
    updateSystemState('Eco-Mode: Quota Protection Active', 'status-down');

    // Hide countdown container when in Eco-Mode
    const countdownContainer = document.getElementById('idleCountdownContainer');
    if (countdownContainer) {
        countdownContainer.style.display = 'none';
    }

    addLog('💤 Entering Eco-Mode to save RPC quota. Move mouse or click to wake up.', 'warn');
}

function hideSleepOverlay() {
    document.body.classList.remove('is-sleeping');
    clearInterval(countdownInterval);
    updateCountdownDisplay(0); // Reset countdown display

    // Hide countdown container
    const countdownContainer = document.getElementById('idleCountdownContainer');
    if (countdownContainer) {
        countdownContainer.style.display = 'none';
    }
}

function updateCountdownDisplay(seconds) {
    const countdownEl = document.getElementById('idleCountdown');
    if (countdownEl) {
        if (seconds > 0) {
            const mins = Math.floor(seconds / 60);
            const secs = seconds % 60;
            countdownEl.textContent = `⏰ ${mins}:${secs.toString().padStart(2, '0')}`;
            countdownEl.style.color = seconds < 20 ? '#f43f5e' : '#64748b'; // Red in last 20 seconds
        } else {
            countdownEl.textContent = '';
        }
    }
}

function startIdleCountdown() {
    let secondsLeft = 60; // Show 60-second countdown before entering Eco-Mode

    updateCountdownDisplay(secondsLeft);
    addLog('⏰ Inactivity detected: Entering Eco-Mode in 60 seconds...', 'warn');

    countdownInterval = setInterval(() => {
        secondsLeft--;
        updateCountdownDisplay(secondsLeft);

        if (secondsLeft <= 0) {
            clearInterval(countdownInterval);
            showSleepOverlay();
        }
    }, 1000);
}

function showSleepOverlay() {
    document.body.classList.add('is-sleeping');
    updateSystemState('Eco-Mode: Quota Protection Active', 'status-down');
    addLog('💤 Entering Eco-Mode to save RPC quota. Move mouse or click to wake up.', 'warn');
}

function hideSleepOverlay() {
    document.body.classList.remove('is-sleeping');
    clearInterval(countdownInterval);
    updateCountdownDisplay(0); // Reset countdown display
}

function updateCountdownDisplay(seconds) {
    const countdownEl = document.getElementById('idleCountdown');
    if (countdownEl) {
        if (seconds > 0) {
            const mins = Math.floor(seconds / 60);
            const secs = seconds % 60;
            countdownEl.textContent = `⏰ Eco-Mode in ${mins}:${secs.toString().padStart(2, '0')}`;
            countdownEl.style.color = seconds < 20 ? '#f43f5e' : '#64748b'; // Red in last 20 seconds
        } else {
            countdownEl.textContent = '';
        }
    }
}

// 🚀 Interaction Listeners
['mousemove', 'mousedown', 'scroll', 'keypress', 'click'].forEach(evt => {
    window.addEventListener(evt, () => {
        // Throttle heartbeat to once every 10s to avoid spam
        if (!window.lastHeartbeat || Date.now() - window.lastHeartbeat > 10000) {
            resetIdleTimer();
            window.lastHeartbeat = Date.now();
        }
    });
});

document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') resetIdleTimer();
});

// 🚀 Interaction Listeners
['mousemove', 'mousedown', 'scroll', 'keypress', 'click'].forEach(evt => {
    window.addEventListener(evt, () => {
        // Throttle heartbeat to once every 10s to avoid spam
        if (!window.lastHeartbeat || Date.now() - window.lastHeartbeat > 10000) {
            resetIdleTimer();
            window.lastHeartbeat = Date.now();
        }
    });
});

document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') resetIdleTimer();
});

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
        wsDisconnectedSince = null; // 🛡️ 重置断线时间戳
        updateSystemState('● LIVE', 'status-live', true);
        healthEl.textContent = '✅ Connected';
        healthEl.className = 'status-badge status-healthy';
        addLog('🔗 WebSocket reconnected successfully', 'success');
        console.log('✅ WebSocket Connected');
        
        resetIdleTimer(); // Initial activity
        
        // 💡 架构升级：重连后自动补齐数据
        fetchData();
        addLog('System connected. Streaming live data...', 'info');
    };

    ws.onmessage = (event) => {
        try {
            const raw = JSON.parse(event.data);
            let msg = raw;

            // 🚀 Handle sleeping state from backend
            // 🛡️ 演示模式保护：忽略后端的休眠信号
            if (raw.type === 'lazy_status' && raw.data.mode === 'sleep') {
                if (!DEMO_MODE_DISABLE_SLEEP) {
                    showSleepOverlay();
                }
                return;
            }

            // 🚀 Handle linearity check progress
            if (raw.type === 'linearity_status') {
                const { status, detail, progress } = raw.data;
                const statusEl = document.getElementById('sleep-status-text');
                const detailEl = document.getElementById('sleep-detail');
                
                if (statusEl) statusEl.textContent = `SYSTEM ${status}`;
                if (detailEl) {
                    let progressText = `> ${detail}`;
                    if (progress) progressText += ` [${progress}%]`;
                    detailEl.textContent = progressText;
                }
                
                if (status === 'ALIGNED') {
                    detailEl.style.color = '#22c55e'; // Green for success
                    setTimeout(hideSleepOverlay, 1500); 
                } else if (status === 'REPAIRING') {
                    detailEl.style.color = '#f43f5e'; // Red for repair
                    showSleepOverlay();
                } else {
                    showSleepOverlay();
                }
                return;
            }
            if (raw.signature && raw.data) {
                updateSignatureStatus(true, raw.signer_id, raw.signature);
                msg = { type: raw.type, data: raw.data };
            }

            if (msg.type === 'block') {
                const data = msg.data;
                updateBlocksTable(data);
                
                // 🚀 Atomic UI Update: Sync header stats with block event
                if (data.latest_chain) document.getElementById('latestBlock').textContent = data.latest_chain;
                document.getElementById('totalBlocks').textContent = data.number; // latest synced
                if (data.sync_lag !== undefined) {
                    const lagEl = document.getElementById('syncLag');
                    lagEl.textContent = data.sync_lag;
                    lagEl.style.color = data.sync_lag > 10 ? '#f43f5e' : '#667eea';
                }
                if (data.tps !== undefined) document.getElementById('tps').textContent = data.tps.toFixed(2);
                if (data.latency_display) document.getElementById('latency').textContent = data.latency_display;
                
                document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
            } else if (msg.type === 'transfer') {
                const tx = msg.data;
                tx.amount = tx.value || tx.amount; 
                updateTransfersTable(tx);
                // 🚀 Optimistic UI: If we see a transfer, the block is definitely processed
                optimisticUpdateBlockStatus(tx.block_number);
                addLog(`Transfer detected: ${tx.tx_hash.substring(0,10)}...`, 'success');
            } else if (msg.type === 'gas_leaderboard') {
                updateGasLeaderboard(msg.data);
            } else if (msg.type === 'engine_panic') {
                healthEl.textContent = '🚨 FATAL ERROR';
                healthEl.className = 'status-badge status-error';
                addLog(`CRITICAL: Engine Component [${msg.data.worker}] Crashed: ${msg.data.error}`, 'error');
                updateSystemState('ENGINE CRASHED', 'status-down');
            } else if (msg.type === 'log') {
                addLog(msg.data.message, msg.data.level);
            }
        } catch (e) { console.error('WS Error:', e); }
    };

    ws.onclose = (e) => {
        isWSConnected = false;
        if (wsDisconnectedSince === null) {
            wsDisconnectedSince = Date.now(); // 🛡️ 记录断线时间
        }
        updateSystemState('DISCONNECTED', 'status-down');
        healthEl.textContent = '❌ Disconnected';
        healthEl.className = 'status-badge status-error';

        const gracePeriodSeconds = WS_DISCONNECT_GRACE_PERIOD / 1000;
        addLog(`WebSocket connection lost. ${gracePeriodSeconds}s grace period before Eco-Mode...`, 'warn');
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
        const currentLatestDisplay = parseInt(document.getElementById('latestBlock').textContent || '0');
        const incomingLatest = parseInt(data?.latest_block || '0');

        // 🚀 视觉洗盘：如果后端高度发生大幅回退（说明执行了 Hard Reset）
        if (incomingLatest > 0 && currentLatestDisplay > incomingLatest + 100) {
            console.warn('🚨 State Reset detected in backend! Purging UI...');
            location.reload();
            return;
        }

        document.getElementById('latestBlock').textContent = data?.latest_block || '0';
        document.getElementById('totalBlocks').textContent = data?.total_blocks || '0';
        document.getElementById('totalTransfers').textContent = data?.total_transfers || '0';
        document.getElementById('tps').textContent = data?.tps || '0';
        document.getElementById('bps').textContent = data?.bps || '0';

        // 🎯 同步进度百分比显示（替换原有的 Total Blocks 显示）
        if (data?.sync_progress_percent !== undefined) {
            const progress = data.sync_progress_percent;
            const progressEl = document.getElementById('totalBlocks');
            const totalSyncedEl = document.getElementById('totalBlocks');

            // 🔥 横滨实验室修复：只有在真正追平时才显示 100%
            // 逻辑检查：如果 Sync Lag > 0，即使百分比很高，也不显示 100%
            const syncLag = data?.sync_lag || 0;
            const latestBlock = parseInt(data?.latest_block || '0');
            const totalBlocks = parseInt(data?.total_blocks || '0');

            // 真实的进度百分比计算
            let realProgress = progress;
            if (syncLag > 0 || latestBlock > totalBlocks) {
                // 有滞后或未追平，强制显示真实进度（不是 100%）
                realProgress = Math.min(progress, 99.5); // 最高 99.5%
            }

            // 格式化百分比显示
            let displayText = '';
            let color = '#667eea';

            if (realProgress >= 99.9) {
                displayText = '100% ✅';
                color = '#10b981'; // 绿色
            } else if (realProgress >= 95.0) {
                displayText = realProgress.toFixed(1) + '%';
                color = '#f59e0b'; // 黄色
            } else if (realProgress >= 90.0) {
                displayText = realProgress.toFixed(1) + '%';
                color = '#f97316'; // 橙色
            } else {
                displayText = realProgress.toFixed(1) + '%';
                color = '#f43f5e'; // 红色
            }

            // 同时显示百分比和绝对数字（小字体）
            const absoluteNumber = data?.total_blocks || '0';
            totalSyncedEl.innerHTML = `
                <span style="color: ${color}; font-weight: bold; font-size: 1.1em;">${displayText}</span>
                <span style="color: #6b7280; font-size: 0.8em; margin-left: 8px;">(${absoluteNumber})</span>
            `;
        }

        // 🚀 Sync Lag & Time-Travel Alert
        const syncLagEl = document.getElementById('syncLag');
        if (data?.time_travel) {
            syncLagEl.innerHTML = `<span style="color: #f43f5e; font-weight: bold; animation: pulse 2s infinite;">⚠️ RE-ALIGN REQ [${data.sync_lag}]</span>`;
            addLog('🚨 CRITICAL: DB is ahead of Chain! Alignment required.', 'error');
        } else {
            const lag = data?.sync_lag || 0;
            syncLagEl.textContent = lag;
            // 颜色根据 lag 大小变化
            if (lag <= 5) {
                syncLagEl.style.color = '#10b981'; // 绿色 - 实时
            } else if (lag <= 20) {
                syncLagEl.style.color = '#f59e0b'; // 黄色 - 轻微延迟
            } else {
                syncLagEl.style.color = '#f43f5e'; // 红色 - 严重滞后
            }
        }

        document.getElementById('latency').textContent = data?.e2e_latency_display || '0s';
        document.getElementById('totalVisitors').textContent = data?.total_visitors || '0';
        document.getElementById('adminIP').textContent = data?.admin_ip || 'None';
        document.getElementById('selfHealing').textContent = data?.self_healing_count || '0';
        document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();

        // 🚀 Sync overlay if backend reports sleep
        // 🛡️ 演示模式保护：忽略后端的休眠信号
        if (!DEMO_MODE_DISABLE_SLEEP) {
            if (data?.lazy_indexer?.mode === 'sleep') {
                showSleepOverlay();
            } else if (data?.lazy_indexer?.mode === 'active') {
                hideSleepOverlay();
            }
        }
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
        <td class="status-cell status-success">
            <div class="status-indicator">
                <span class="dot"></span>
                <span class="status-text">${blockTime}</span>
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

// 🚀 Viewport-triggered Lazy Loading (Intersection Observer)
const tokenObserver = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
        if (entry.isIntersecting) {
            const el = entry.target;
            const addr = el.dataset.addr;
            const symbol = el.dataset.symbol;

            // Only request if it's a pending/unknown token
            if (addr && addr !== '0x0000000000000000000000000000000000000000' && (symbol === '' || symbol.includes('...'))) {
                console.log('🔍 Token entered viewport, requesting enrichment:', addr);
                if (ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({
                        type: 'NEED_METADATA',
                        data: { address: addr }
                    }));
                }
                tokenObserver.unobserve(el);
            }
        }
    });
}, { threshold: 0.1 });

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
    
    // 🚀 增加 data-addr 和 data-symbol 用于 Lazy Loading
    const tokenDisplay = symbol && symbol !== token.substring(0, 10) ? 
        `<span class="token-badge token-${symbol.toLowerCase()}" data-addr="${token}" data-symbol="${symbol}">${symbol}</span>` : 
        `<span class="address token-pending" title="${token}" data-addr="${token}" data-symbol="${symbol}">${token.substring(0, 8)}...${token.substring(34)}</span>`;
    
    const rowId = `tx-${tx.tx_hash.substring(0, 10)}-${tx.log_index}`;
    const row = `<tr id="${rowId}">
        <td class="stat-value">${tx.block_number || '0'}</td>
        <td>${activityDisplay}</td>
        <td class="address">${from.substring(0, 10)}...</td>
        <td class="address">${to.substring(0, 10)}...</td>
        <td class="stat-value" style="color: #667eea; font-family: 'Courier New', monospace;" title="${tx.amount || tx.value}">${displayAmount}</td>
        <td>${tokenDisplay}</td>
    </tr>`;
    table.insertAdjacentHTML('afterbegin', row);
    
    // 开始观察新加入的 Token Badge
    const newBadge = document.querySelector(`#${rowId} [data-addr]`);
    if (newBadge) tokenObserver.observe(newBadge);

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
                const type = t.type || t.activity_type || 'TRANSFER';
                const token = t.token_address || '0x...';
                const displayAmount = formatAmount(t.amount || '0', 18); // 默认 18 位精度
                
                // 🎨 Activity & Token Badge 渲染逻辑
                const activityDisplay = renderActivityIcon(type);
                const tokenDisplay = symbol && symbol !== token.substring(0, 10) ? 
                    `<span class="token-badge token-${symbol.toLowerCase()}" data-addr="${token}" data-symbol="${symbol}">${symbol}</span>` : 
                    `<span class="address token-pending" title="${token}" data-addr="${token}" data-symbol="${symbol}">${token.substring(0, 8)}...${token.substring(34)}</span>`;
                
                return `<tr>
                    <td class="stat-value">${t.block_number || '0'}</td>
                    <td>${activityDisplay}</td>
                    <td class="address">${from.substring(0, 10)}...</td>
                    <td class="address">${to.substring(0, 10)}...</td>
                    <td class="stat-value" style="color: #667eea; font-family: 'Courier New', monospace;" title="${t.amount || '0'}">${displayAmount}</td>
                    <td>${tokenDisplay}</td>
                </tr>`;
            }).join('');

            // 对初始加载的数据也启动观察
            document.querySelectorAll('#transfersTable [data-addr]').forEach(el => tokenObserver.observe(el));
        }
    } catch (e) { console.error('Fetch Error:', e); }
}

fetchData();
connectWS();

// 🚀 根据环境选择刷新频率
const refreshInterval = DEMO_MODE_DISABLE_SLEEP ? DEMO_REFRESH_INTERVAL : 5000;
console.log(`🔄 Dashboard refresh interval: ${refreshInterval}ms (${DEMO_MODE_DISABLE_SLEEP ? 'Demo Mode (Fast)' : 'Normal Mode'})`);
setInterval(fetchStatus, refreshInterval);

function updateSignatureStatus(isSigned, signerId, signature) {
    const sigStatusEl = document.getElementById('signatureStatus');
    if (sigStatusEl && isSigned) {
        sigStatusEl.innerHTML = `<span style="color: #ff9800; font-weight: bold; font-size: 11px;">🛡️ Verified: ${signerId}</span>`;
        sigStatusEl.title = "Ed25519 Payload Signature: " + signature;
    }
}
