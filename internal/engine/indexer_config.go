package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"web3-indexer-go/internal/limiter"
)

// SyncMode defines the indexer's resource/speed trade-off policy.
type SyncMode string

const (
	// SyncModeAggressive: full-speed sync, ignores quota warnings.
	// Use for initial catch-up when lag > 10000 blocks.
	SyncModeAggressive SyncMode = "aggressive"

	// SyncModeBalanced: dynamically adjusts BPS based on remaining RPC quota.
	// Default for daily production use.
	SyncModeBalanced SyncMode = "balanced"

	// SyncModeEco: wakes only on visitor/manual trigger; minimal quota use.
	// Current behaviour of LazyManager when isAlwaysActive=false.
	SyncModeEco SyncMode = "eco"
)

// IndexerConfig holds all runtime-tunable parameters for the indexer engine.
// It is designed for hot-reload via SIGHUP or fsnotify without restart.
//
// JSON tags match the /api/config request/response body for UI integration.
type IndexerConfig struct {
	// SyncMode controls the overall resource/speed policy.
	SyncMode SyncMode `json:"sync_mode"`

	// RpcQuotaThreshold is the fraction of the sliding-window quota (0.0–1.0)
	// at which the engine transitions from Balanced → Eco throttle.
	// Corresponds to SlidingWindowLimiter.ecoThreshold.
	// Default: 0.80 (enter Eco when 80% of per-minute quota is consumed).
	RpcQuotaThreshold float64 `json:"rpc_quota_threshold"`

	// RpcBalancedThreshold is the fraction at which Aggressive → Balanced.
	// Default: 0.50.
	RpcBalancedThreshold float64 `json:"rpc_balanced_threshold"`

	// RpcWindowLimit is the maximum RPC requests allowed per RpcWindowDuration.
	// Set this to your provider's per-minute CU/request limit.
	// Default: 300 (Alchemy free tier: 300 req/min on Sepolia).
	RpcWindowLimit int `json:"rpc_window_limit"`

	// RpcWindowDuration is the quota measurement window.
	// Default: 60s (matches most providers' reset period).
	RpcWindowDuration time.Duration `json:"rpc_window_duration"`

	// MaxRPS is the hard ceiling on outbound RPC requests per second.
	// The sliding window limiter's RecommendedRPS() will never exceed this.
	// Default: 15 for Sepolia testnet, 500 for local Anvil.
	MaxRPS float64 `json:"max_rps"`

	// IdleTimeout is how long without a heartbeat before LazyManager
	// transitions Active → Sleep (Eco mode only).
	// Default: 5 minutes.
	IdleTimeout time.Duration `json:"idle_timeout"`

	// AnomalySensitivity controls how aggressively the DeadlockWatchdog
	// triggers self-healing. Lower = more sensitive.
	//   1.0 = trigger when gap > 1000 blocks (current default)
	//   0.5 = trigger when gap > 500 blocks
	//   2.0 = trigger when gap > 2000 blocks
	AnomalySensitivity float64 `json:"anomaly_sensitivity"`

	// AutoWakeupEvent specifies which events automatically wake the engine
	// from Eco sleep without a manual HTTP trigger.
	// Supported values: "none", "new_block", "new_tx", "wss_event"
	AutoWakeupEvent string `json:"auto_wakeup_event"`

	// FetcherConcurrency is the number of parallel block-fetch workers.
	// Increasing this without raising MaxRPS will cause more 429s.
	// Default: 4.
	FetcherConcurrency int `json:"fetcher_concurrency"`

	// BatchSize is the number of blocks per FilterLogs range query.
	// Larger = fewer RPC calls but higher per-call latency and 429 risk.
	// Default: 50.
	BatchSize int `json:"batch_size"`

	// CheckpointBatch is how many blocks to process before persisting
	// sync_checkpoints. Lower = more durable, higher = faster sync.
	// Default: 100.
	CheckpointBatch int `json:"checkpoint_batch"`

	// DemoMode enables Leap-Sync and gap-skip in ConsistencyGuard/Sequencer.
	// Should be false in production to preserve data completeness.
	DemoMode bool `json:"demo_mode"`

	// AlwaysActive maps to LazyManager.SetAlwaysActive().
	// When true, Eco-Mode hibernation is fully disabled.
	AlwaysActive bool `json:"always_active"`
}

// DefaultConfig returns safe defaults for a Sepolia testnet environment.
func DefaultConfig() IndexerConfig {
	return IndexerConfig{
		SyncMode:             SyncModeBalanced,
		RpcQuotaThreshold:    0.80,
		RpcBalancedThreshold: 0.50,
		RpcWindowLimit:       300,
		RpcWindowDuration:    60 * time.Second,
		MaxRPS:               15.0,
		IdleTimeout:          5 * time.Minute,
		AnomalySensitivity:   1.0,
		AutoWakeupEvent:      "new_block",
		FetcherConcurrency:   4,
		BatchSize:            50,
		CheckpointBatch:      100,
		DemoMode:             false,
		AlwaysActive:         false,
	}
}

// LocalLabConfig returns aggressive defaults for the RX 9070 XT / 128G RAM
// local lab environment (Anvil or local Sepolia node).
func LocalLabConfig() IndexerConfig {
	cfg := DefaultConfig()
	cfg.SyncMode = SyncModeAggressive
	cfg.RpcWindowLimit = 100000 // local node: effectively unlimited
	cfg.MaxRPS = 500.0
	cfg.IdleTimeout = 30 * time.Minute
	cfg.FetcherConcurrency = 16
	cfg.BatchSize = 200
	cfg.CheckpointBatch = 500
	cfg.AlwaysActive = true
	return cfg
}

// ConfigManager manages the live IndexerConfig with hot-reload support.
// It is the single source of truth for all runtime-tunable parameters.
type ConfigManager struct {
	mu      sync.RWMutex
	current IndexerConfig
	path    string // optional: path to JSON config file for fsnotify reload

	// onChange is called after every successful config update.
	// Callers (e.g. initEngine) register their apply functions here.
	onChange []func(cfg IndexerConfig)

	logger *slog.Logger
}

// NewConfigManager creates a ConfigManager with the given initial config.
func NewConfigManager(initial IndexerConfig) *ConfigManager {
	return &ConfigManager{
		current: initial,
		logger:  slog.Default(),
	}
}

// NewConfigManagerFromEnv creates a ConfigManager, auto-detecting local vs testnet.
func NewConfigManagerFromEnv() *ConfigManager {
	cfg := DefaultConfig()

	// Auto-detect local lab environment
	for _, envVar := range []string{"RPC_URLS", "RPC_URL", "DATABASE_URL"} {
		val := os.Getenv(envVar)
		if val == "" {
			continue
		}
		for _, local := range []string{"localhost", "127.0.0.1", "anvil"} {
			if strings.Contains(val, local) {
				cfg = LocalLabConfig()
				break
			}
		}
	}

	// Override with environment variables for CI/CD compatibility
	if v := os.Getenv("SYNC_MODE"); v != "" {
		cfg.SyncMode = SyncMode(v)
	}
	if os.Getenv("ALWAYS_ACTIVE") == "true" {
		cfg.AlwaysActive = true
	}
	if os.Getenv("DEMO_MODE") == "true" {
		cfg.DemoMode = true
	}

	return NewConfigManager(cfg)
}

// Get returns a snapshot of the current config (safe for concurrent reads).
func (cm *ConfigManager) Get() IndexerConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.current
}

// Update atomically replaces the config and notifies all registered listeners.
// This is the hot-reload entry point called by SIGHUP handler or /api/config PUT.
func (cm *ConfigManager) Update(ctx context.Context, next IndexerConfig) error {
	if err := validateConfig(next); err != nil {
		return err
	}

	cm.mu.Lock()
	cm.current = next
	listeners := make([]func(IndexerConfig), len(cm.onChange))
	copy(listeners, cm.onChange)
	cm.mu.Unlock()

	cm.logger.Info("config_updated",
		slog.String("sync_mode", string(next.SyncMode)),
		slog.Float64("rpc_quota_threshold", next.RpcQuotaThreshold),
		slog.Float64("max_rps", next.MaxRPS),
		slog.Bool("always_active", next.AlwaysActive),
	)

	// Notify listeners outside the lock to avoid deadlock
	for _, fn := range listeners {
		fn(next)
	}
	return nil
}

// OnChange registers a callback invoked after every successful Update().
// Typical usage: register LazyManager.SetAlwaysActive, Fetcher.SetThroughputLimit, etc.
func (cm *ConfigManager) OnChange(fn func(cfg IndexerConfig)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onChange = append(cm.onChange, fn)
}

// MarshalJSON serialises the current config for the /api/config GET response.
func (cm *ConfigManager) MarshalJSON() ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return json.Marshal(cm.current)
}

// BuildSlidingWindowLimiter constructs a SlidingWindowLimiter from the current config.
// Call this when initialising or re-initialising the RPC pool.
func (cm *ConfigManager) BuildSlidingWindowLimiter() *limiter.SlidingWindowLimiter {
	cfg := cm.Get()
	swl := limiter.NewSlidingWindowLimiter(cfg.RpcWindowLimit, cfg.RpcWindowDuration)
	swl.SetThresholds(cfg.RpcBalancedThreshold, cfg.RpcQuotaThreshold)
	return swl
}

// AnomalyGapThreshold returns the block gap size that triggers DeadlockWatchdog
// self-healing, derived from AnomalySensitivity.
func (cm *ConfigManager) AnomalyGapThreshold() int64 {
	cfg := cm.Get()
	base := int64(1000)
	return int64(float64(base) * cfg.AnomalySensitivity)
}

// validateConfig checks for obviously invalid values.
func validateConfig(cfg IndexerConfig) error {
	if cfg.RpcQuotaThreshold <= 0 || cfg.RpcQuotaThreshold > 1.0 {
		return errorf("rpc_quota_threshold must be in (0, 1], got %f", cfg.RpcQuotaThreshold)
	}
	if cfg.RpcBalancedThreshold <= 0 || cfg.RpcBalancedThreshold >= cfg.RpcQuotaThreshold {
		return errorf("rpc_balanced_threshold must be in (0, rpc_quota_threshold), got %f", cfg.RpcBalancedThreshold)
	}
	if cfg.MaxRPS <= 0 {
		return errorf("max_rps must be > 0, got %f", cfg.MaxRPS)
	}
	if cfg.RpcWindowLimit <= 0 {
		return errorf("rpc_window_limit must be > 0, got %d", cfg.RpcWindowLimit)
	}
	if cfg.FetcherConcurrency <= 0 {
		return errorf("fetcher_concurrency must be > 0, got %d", cfg.FetcherConcurrency)
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 2000 {
		return errorf("batch_size must be in [1, 2000], got %d", cfg.BatchSize)
	}
	switch cfg.SyncMode {
	case SyncModeAggressive, SyncModeBalanced, SyncModeEco:
	default:
		return errorf("unknown sync_mode: %q", cfg.SyncMode)
	}
	return nil
}

func errorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}
