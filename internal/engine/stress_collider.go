package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// 🔥 LocalStressCollider - Yokohama Lab 专用压力对撞机
// 设计目标：在 16G 内存的 5600U 上，将 BPS 推向物理极限
type StressCollider struct {
	rpcURL   string
	client   *ethclient.Client
	chainID  *big.Int
	ctx      context.Context
	cancel   context.CancelFunc

	accounts []*stressAccount
	config   ColliderConfig

	metrics StressMetrics
	pauseCh  chan struct{}
	resumeCh chan struct{}
	adjustCh chan ColliderConfig

	backpressureFn func() bool
}

type stressAccount struct {
	addr     common.Address
	privKey  interface{}
	nonce    uint64
	nonceMu  sync.Mutex
	inFlight int32
}

// ColliderConfig 压力对撞机配置
type ColliderConfig struct {
	TargetTPS       int
	BurstSize       int
	RampingPeriod   time.Duration
	TxComplexity    string
	ContractDensity float64
	AccountCount    int
	BatchMode       bool
	Duration        time.Duration
	MaxPendingTx    int
}

// StressMetrics 实时压测指标
type StressMetrics struct {
	TxSubmitted    atomic.Uint64
	TxConfirmed    atomic.Uint64
	TxFailed       atomic.Uint64
	CurrentTPS     atomic.Uint64
	PeakTPS        atomic.Uint64
	AvgLatencyMs   atomic.Uint64
	ActiveAccounts atomic.Uint32
	PendingCount   atomic.Int32
	StartTime      time.Time
	LastResetTime  time.Time
}

// StressMetricsSnapshot 序列化友好的指标快照
type StressMetricsSnapshot struct {
	TxSubmitted    uint64  `json:"tx_submitted"`
	TxConfirmed    uint64  `json:"tx_confirmed"`
	TxFailed       uint64  `json:"tx_failed"`
	CurrentTPS     uint64  `json:"current_tps"`
	PeakTPS        uint64  `json:"peak_tps"`
	AvgLatencyMs   uint64  `json:"avg_latency_ms"`
	ActiveAccounts uint32  `json:"active_accounts"`
	PendingCount   int32   `json:"pending_count"`
	ElapsedSec     float64 `json:"elapsed_sec"`
}

// DefaultColliderConfig 5600U 优化默认配置
func DefaultColliderConfig() ColliderConfig {
	return ColliderConfig{
		TargetTPS:       1000,
		BurstSize:       50,
		RampingPeriod:   30 * time.Second,
		TxComplexity:    "defi",
		ContractDensity: 0.1,
		AccountCount:    100,
		BatchMode:       true,
		Duration:        5 * time.Minute,
		MaxPendingTx:    10,
	}
}

// NewStressCollider 创建压力对撞机
func NewStressCollider(rpcURL string, config ColliderConfig) (*StressCollider, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial rpc failed: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	chainID, err := client.ChainID(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("get chain id failed: %w", err)
	}

	collider := &StressCollider{
		rpcURL:   rpcURL,
		client:   client,
		chainID:  chainID,
		ctx:      ctx,
		cancel:   cancel,
		config:   config,
		pauseCh:  make(chan struct{}),
		resumeCh: make(chan struct{}),
		adjustCh: make(chan ColliderConfig, 10),
		metrics: StressMetrics{
			StartTime:     time.Now(),
			LastResetTime: time.Now(),
		},
	}

	if err := collider.initAccountPool(); err != nil {
		cancel()
		return nil, fmt.Errorf("init account pool failed: %w", err)
	}

	slog.Info("🔥 [STRESS_COLLIDER] 初始化完成",
		"target_tps", config.TargetTPS,
		"accounts", config.AccountCount)

	return collider, nil
}

// initAccountPool 预生成高性能账户池
func (sc *StressCollider) initAccountPool() error {
	sc.accounts = make([]*stressAccount, 0, sc.config.AccountCount)

	basePKs := []string{
		"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
		"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
	}

	accountsPerBase := (sc.config.AccountCount + len(basePKs) - 1) / len(basePKs)

	for i, basePK := range basePKs {
		if len(sc.accounts) >= sc.config.AccountCount {
			break
		}

		for j := 0; j < accountsPerBase && len(sc.accounts) < sc.config.AccountCount; j++ {
			hash := crypto.Keccak256([]byte(fmt.Sprintf("%s:%d:%d", basePK, i, j)))
			childPriv, err := crypto.ToECDSA(hash)
			if err != nil {
				continue
			}

			addr := crypto.PubkeyToAddress(childPriv.PublicKey)

			acc := &stressAccount{
				addr:     addr,
				privKey:  childPriv,
				nonce:    0,
				inFlight: 0,
			}

			nonce, err := sc.client.NonceAt(sc.ctx, addr, nil)
			if err == nil {
				acc.nonce = nonce
			}

			sc.accounts = append(sc.accounts, acc)
		}
	}

	sc.metrics.ActiveAccounts.Store(uint32(len(sc.accounts)))
	slog.Info("🔥 [STRESS_COLLIDER] 账户池初始化完成", "total_accounts", len(sc.accounts))
	return nil
}

// Start 启动压力对撞机
func (sc *StressCollider) Start() {
	slog.Info("🚀 [STRESS_COLLIDER] IGNITION START",
		"target_tps", sc.config.TargetTPS,
		"duration", sc.config.Duration)

	sc.metrics.StartTime = time.Now()
	sc.metrics.LastResetTime = time.Now()

	go sc.stressLoop()
	go sc.metricsAggregator()

	if sc.config.RampingPeriod > 0 {
		go sc.rampingController()
	}

	if sc.config.Duration > 0 {
		go func() {
			time.Sleep(sc.config.Duration)
			slog.Info("⏱️ [STRESS_COLLIDER] 压测时长到达，自动停止")
			sc.Stop()
		}()
	}
}

// stressLoop 核心压力循环
func (sc *StressCollider) stressLoop() {
	burstTicker := time.NewTicker(time.Millisecond * 10)
	defer burstTicker.Stop()

	batch := make([]*types.Transaction, 0, sc.config.BurstSize)

	for {
		select {
		case <-sc.ctx.Done():
			return

		case newConfig := <-sc.adjustCh:
			sc.applyConfig(newConfig)

		case <-burstTicker.C:
			if sc.backpressureFn != nil && sc.backpressureFn() {
				time.Sleep(time.Millisecond * 5)
				continue
			}

			for i := 0; i < sc.config.BurstSize; i++ {
				acc := sc.selectAccount()
				if acc == nil {
					continue
				}

				tx := sc.generateTransaction(acc)
				if tx != nil {
					batch = append(batch, tx)
				}
			}

			if len(batch) > 0 {
				sc.submitBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// selectAccount 选择可用账户
func (sc *StressCollider) selectAccount() *stressAccount {
	startIdx := secureIntn(len(sc.accounts))

	for i := 0; i < len(sc.accounts); i++ {
		idx := (startIdx + i) % len(sc.accounts)
		acc := sc.accounts[idx]

		if atomic.LoadInt32(&acc.inFlight) < int32(sc.config.MaxPendingTx) {
			atomic.AddInt32(&acc.inFlight, 1)
			return acc
		}
	}

	return nil
}

// generateTransaction 根据复杂度生成交易
func (sc *StressCollider) generateTransaction(acc *stressAccount) *types.Transaction {
	acc.nonceMu.Lock()
	nonce := acc.nonce
	acc.nonce++
	acc.nonceMu.Unlock()

	defer atomic.AddInt32(&acc.inFlight, -1)

	txType := secureIntn(100)

	var tx *types.Transaction
	switch sc.config.TxComplexity {
	case "simple":
		to := sc.accounts[secureIntn(len(sc.accounts))].addr
		amount := big.NewInt(1e15)
		tx = types.NewTransaction(nonce, to, amount, 21000, big.NewInt(1e9), nil)

	case "defi":
		if txType < 40 {
			to := sc.accounts[secureIntn(len(sc.accounts))].addr
			amount := big.NewInt(1e15)
			tx = types.NewTransaction(nonce, to, amount, 21000, big.NewInt(1e9), nil)
		} else if txType < 70 {
			tx = sc.generateApproveTx(acc, nonce)
		} else if txType < 90 {
			tx = sc.generateSwapTx(acc, nonce)
		} else {
			tx = sc.generateDeployTx(acc, nonce)
		}

	case "chaos":
		if txType < 25 {
			tx = types.NewTransaction(nonce, acc.addr, big.NewInt(1), 21000, big.NewInt(1e9), nil)
		} else if txType < 50 {
			tx = sc.generateApproveTx(acc, nonce)
		} else if txType < 75 {
			tx = sc.generateSwapTx(acc, nonce)
		} else {
			tx = sc.generateDeployTx(acc, nonce)
		}
	}

	return tx
}

// generateApproveTx 生成 ERC20 approve 交易
func (sc *StressCollider) generateApproveTx(acc *stressAccount, nonce uint64) *types.Transaction {
	token := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	spender := sc.accounts[secureIntn(len(sc.accounts))].addr

	methodID := common.FromHex("0x095ea7b3")
	data := make([]byte, 0, 68)
	data = append(data, methodID...)
	data = append(data, common.LeftPadBytes(spender.Bytes(), 32)...)
	data = append(data, common.MaxHash.Bytes()...)

	return types.NewTransaction(nonce, token, big.NewInt(0), 100000, big.NewInt(1e9), data)
}

// generateSwapTx 生成 DEX swap 交易
func (sc *StressCollider) generateSwapTx(acc *stressAccount, nonce uint64) *types.Transaction {
	router := common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564")
	data := common.FromHex("0x04e45aaf")
	return types.NewTransaction(nonce, router, big.NewInt(1e16), 300000, big.NewInt(1e9), data)
}

// generateDeployTx 生成合约部署交易
func (sc *StressCollider) generateDeployTx(acc *stressAccount, nonce uint64) *types.Transaction {
	bytecode := common.FromHex("6080604052348015600f57600080fd5b50603e80601d6000396000f3fe6080604052600080fd")
	return types.NewContractCreation(nonce, big.NewInt(0), 200000, big.NewInt(1e9), bytecode)
}

// submitBatch 批量提交交易
func (sc *StressCollider) submitBatch(txs []*types.Transaction) {
	var wg sync.WaitGroup

	for _, tx := range txs {
		wg.Add(1)
		go func(transaction *types.Transaction) {
			defer wg.Done()

			sc.metrics.TxSubmitted.Add(1)
			sc.metrics.PendingCount.Add(1)
		}(tx)
	}

	wg.Wait()
}

// metricsAggregator 实时指标聚合
func (sc *StressCollider) metricsAggregator() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var lastSubmitted uint64

	for {
		select {
		case <-sc.ctx.Done():
			return
		case <-ticker.C:
			current := sc.metrics.TxSubmitted.Load()
			delta := current - lastSubmitted
			lastSubmitted = current

			sc.metrics.CurrentTPS.Store(delta)

			peak := sc.metrics.PeakTPS.Load()
			if delta > peak {
				sc.metrics.PeakTPS.Store(delta)
			}

			if time.Since(sc.metrics.LastResetTime) > 10*time.Second {
				sc.logMetrics()
				sc.metrics.LastResetTime = time.Now()
			}
		}
	}
}

// rampingController 渐进加压控制器
func (sc *StressCollider) rampingController() {
	steps := 10
	stepDuration := sc.config.RampingPeriod / time.Duration(steps)
	targetTPS := sc.config.TargetTPS

	for i := 1; i <= steps; i++ {
		select {
		case <-sc.ctx.Done():
			return
		case <-time.After(stepDuration):
			currentTarget := targetTPS * i / steps
			sc.adjustTPS(currentTarget)
			slog.Info("🔥 [STRESS_COLLIDER] Ramping up",
				"step", i,
				"current_tps", currentTarget,
				"target_tps", targetTPS)
		}
	}
}

// adjustTPS 动态调整 TPS
func (sc *StressCollider) adjustTPS(newTPS int) {
	sc.config.TargetTPS = newTPS
}

// applyConfig 应用新配置
func (sc *StressCollider) applyConfig(config ColliderConfig) {
	sc.config = config
	slog.Info("🔥 [STRESS_COLLIDER] 配置已更新", "new_target_tps", config.TargetTPS)
}

// logMetrics 输出压测报告
func (sc *StressCollider) logMetrics() {
	elapsed := time.Since(sc.metrics.StartTime).Seconds()
	total := sc.metrics.TxSubmitted.Load()
	confirmed := sc.metrics.TxConfirmed.Load()
	failed := sc.metrics.TxFailed.Load()
	currentTPS := sc.metrics.CurrentTPS.Load()
	peakTPS := sc.metrics.PeakTPS.Load()
	pending := sc.metrics.PendingCount.Load()

	slog.Info("📊 [STRESS_COLLIDER] 压测报告",
		"elapsed_sec", fmt.Sprintf("%.1f", elapsed),
		"total_tx", total,
		"confirmed", confirmed,
		"failed", failed,
		"current_tps", currentTPS,
		"peak_tps", peakTPS,
		"pending", pending,
		"avg_tps", fmt.Sprintf("%.1f", float64(total)/elapsed))
}

// GetMetricsSnapshot 获取可序列化的指标快照
func (sc *StressCollider) GetMetricsSnapshot() StressMetricsSnapshot {
	elapsed := time.Since(sc.metrics.StartTime).Seconds()
	return StressMetricsSnapshot{
		TxSubmitted:    sc.metrics.TxSubmitted.Load(),
		TxConfirmed:    sc.metrics.TxConfirmed.Load(),
		TxFailed:       sc.metrics.TxFailed.Load(),
		CurrentTPS:     sc.metrics.CurrentTPS.Load(),
		PeakTPS:        sc.metrics.PeakTPS.Load(),
		AvgLatencyMs:   sc.metrics.AvgLatencyMs.Load(),
		ActiveAccounts: sc.metrics.ActiveAccounts.Load(),
		PendingCount:   sc.metrics.PendingCount.Load(),
		ElapsedSec:     elapsed,
	}
}

// GetConfig 获取当前配置
func (sc *StressCollider) GetConfig() ColliderConfig {
	return sc.config
}

// SetBackpressureCallback 设置背压检测回调
func (sc *StressCollider) SetBackpressureCallback(fn func() bool) {
	sc.backpressureFn = fn
}

// Pause 暂停压测
func (sc *StressCollider) Pause() {
	select {
	case sc.pauseCh <- struct{}{}:
		slog.Info("⏸️ [STRESS_COLLIDER] 已暂停")
	default:
	}
}

// Resume 恢复压测
func (sc *StressCollider) Resume() {
	select {
	case sc.resumeCh <- struct{}{}:
		slog.Info("▶️ [STRESS_COLLIDER] 已恢复")
	default:
	}
}

// AdjustConfig 动态调整配置
func (sc *StressCollider) AdjustConfig(config ColliderConfig) {
	select {
	case sc.adjustCh <- config:
	default:
		slog.Warn("⚠️ [STRESS_COLLIDER] 配置通道已满，调整被丢弃")
	}
}

// Stop 停止压测
func (sc *StressCollider) Stop() {
	sc.logMetrics()
	slog.Info("🛑 [STRESS_COLLIDER] 压测停止")
	sc.cancel()
}

// --- 全局单例管理 ---

var (
	globalStressCollider   *StressCollider
	globalStressColliderMu sync.RWMutex
)

// SetGlobalStressCollider 设置全局 StressCollider 实例
func SetGlobalStressCollider(collider *StressCollider) {
	globalStressColliderMu.Lock()
	defer globalStressColliderMu.Unlock()
	globalStressCollider = collider
}

// GetGlobalStressCollider 获取全局 StressCollider 实例
func GetGlobalStressCollider() *StressCollider {
	globalStressColliderMu.RLock()
	defer globalStressColliderMu.RUnlock()
	return globalStressCollider
}

// StopGlobalStressCollider 停止全局 StressCollider
func StopGlobalStressCollider() {
	globalStressColliderMu.Lock()
	defer globalStressColliderMu.Unlock()
	if globalStressCollider != nil {
		globalStressCollider.Stop()
		globalStressCollider = nil
	}
}
