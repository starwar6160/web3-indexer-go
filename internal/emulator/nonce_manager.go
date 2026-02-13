package emulator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// NonceManager 负责管理账户的 Nonce，确保高频发送下的顺序性与一致性
type NonceManager struct {
	client       *ethclient.Client
	address      common.Address
	mu           sync.Mutex
	pendingNonce uint64
	logger       *slog.Logger
}

func NewNonceManager(client *ethclient.Client, addr common.Address, logger *slog.Logger) (*NonceManager, error) {
	nonce, err := client.PendingNonceAt(context.Background(), addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get initial nonce: %w", err)
	}
	return &NonceManager{
		client:       client,
		address:      addr,
		pendingNonce: nonce,
		logger:       logger,
	}, nil
}

func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// 偶尔与链上同步，防止漂移 (每 50 笔交易强制校验一次)
	if nm.pendingNonce%50 == 0 {
		currentChainNonce, err := nm.client.PendingNonceAt(ctx, nm.address)
		if err == nil && currentChainNonce > nm.pendingNonce {
			nm.logger.Warn("🔍 NONCE_DRIFT_DETECTED_AUTO_FIXING",
				slog.Uint64("local", nm.pendingNonce),
				slog.Uint64("chain", currentChainNonce),
			)
			nm.pendingNonce = currentChainNonce
		}
	}

	nonce := nm.pendingNonce
	nm.pendingNonce++
	return nonce, nil
}

// RollbackNonce 在发送彻底失败时回滚 Nonce (实验性)
func (nm *NonceManager) RollbackNonce(failedNonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if failedNonce < nm.pendingNonce {
		nm.pendingNonce = failedNonce
		nm.logger.Info("🔄 NONCE_ROLLBACK", slog.Uint64("target", failedNonce))
	}
}

func (nm *NonceManager) ResyncNonce(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err != nil {
		return err
	}
	nm.pendingNonce = nonce
	nm.logger.Info("✅ NONCE_RESYNCED", slog.Uint64("new_nonce", nonce))
	return nil
}