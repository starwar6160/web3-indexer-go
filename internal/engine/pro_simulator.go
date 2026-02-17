package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// ProSimulator 工业级持续模拟器
// 直接通过 RPC 向 Anvil 发送交易，无需签名
type ProSimulator struct {
	rpcURL      string
	enabled      bool
	tps          int           // 每秒交易数
	ctx          context.Context
	cancel       context.CancelFunc
	tokens       []TokenInfo
	accounts     []common.Address
}

// NewProSimulator 创建 Pro 模拟器
func NewProSimulator(rpcURL string, enabled bool, tps int) *ProSimulator {
	ctx, cancel := context.WithCancel(context.Background())

	return &ProSimulator{
		rpcURL:  rpcURL,
		enabled:  enabled,
		tps:     tps,
		ctx:     ctx,
		cancel:  cancel,
		tokens: []TokenInfo{
			{common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), "USDC", 6, 1.0},
			{common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), "USDT", 6, 1.0},
			{common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), "WBTC", 8, 45000.0},
			{common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"), "WETH", 18, 3000.0},
			{common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"), "DAI", 18, 1.0},
		},
		accounts: []common.Address{
			common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"), // Anvil #0
			common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"), // Anvil #1
			common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"), // Anvil #2
			common.HexToAddress("0x90F79bf6EB2c4f870365E785982E1f101E93b906"), // Anvil #3
		},
	}
}

// Start 启动模拟器
func (s *ProSimulator) Start() {
	if !s.enabled {
		slog.Info("🏭 Pro Simulator disabled")
		return
	}

	slog.Info("🚀 Starting Pro Simulator",
		"rpc", s.rpcURL,
		"tps", s.tps,
		"tokens", len(s.tokens),
		"accounts", len(s.accounts))

	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(s.tps))
		defer ticker.Stop()

		seqNum := 0
		for {
			select {
			case <-s.ctx.Done():
				slog.Info("🛑 Pro Simulator stopped")
				return
			case <-ticker.C:
				if err := s.sendRandomTransfer(seqNum); err != nil {
					slog.Error("Failed to send transfer", "error", err)
				}
				seqNum++
			}
		}
	}()
}

// sendRandomTransfer 发送随机 ERC20 Transfer
func (s *ProSimulator) sendRandomTransfer(seqNum int) error {
	// 🚀 Anvil 模式：直接挖矿，触发 Processor 的 Synthetic Transfer 逻辑
	// 这样可以确保 Anvil 链持续产生新区块

	client := &http.Client{Timeout: 5 * time.Second}
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "anvil_mine",
		"params":  []interface{}{hexutil.EncodeUint64(1)}, // 挖 1 个块
		"id":      seqNum,
	})

	resp, err := client.Post(s.rpcURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("RPC returned status %d", resp.StatusCode)
	}

	var result []string
	json.NewDecoder(resp.Body).Decode(&result)

	if seqNum%10 == 0 {
		slog.Info("✨ [PRO] Mined block",
			"seq", seqNum,
			"blocks", len(result),
			"trigger", "synthetic_transfer_generation")
	}

	return nil
}

// formatAmount 格式化金额（考虑精度）
func (s *ProSimulator) formatAmount(amount float64, decimals int) *big.Int {
	// 转换为整数（考虑代币精度）
	multiplier := new(big.Float).SetFloat64(amount)
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	multiplier.Mul(multiplier, precision)

	result := new(big.Int)
	multiplier.Int(result)
	return result
}

// SetTPS 动态调整 TPS
func (s *ProSimulator) SetTPS(tps int) {
	s.tps = tps
	slog.Info("🏭 Pro Simulator TPS updated", "new_tps", tps)
}

// Stop 停止模拟器
func (s *ProSimulator) Stop() {
	s.cancel()
}
