package limiter

import (
	"context"
	"sync"
	"time"
)

// SlidingWindowLimiter implements a sliding window rate limiter.
// Unlike token bucket (golang.org/x/time/rate), this tracks actual
// request timestamps, giving precise quota consumption visibility
// and enabling adaptive throttling without full hibernation.
//
// Design rationale for this codebase:
//   - Replaces the binary Sleep/Wake model in LazyManager with
//     continuous gradient throttling (Aggressive → Balanced → Eco)
//   - Window = 1 minute matches most RPC provider quota reset periods
//   - Thread-safe for use across concurrent Fetcher workers
type SlidingWindowLimiter struct {
	mu          sync.Mutex
	window      time.Duration // quota measurement window (e.g. 1 minute)
	limit       int           // max requests per window
	timestamps  []time.Time   // ring buffer of request timestamps
	head        int           // ring buffer head index
	count       int           // current number of valid timestamps in window
	throttleRPS float64       // current effective RPS (adaptive)

	// Adaptive throttle thresholds
	ecoThreshold      float64 // fraction of quota used to enter Eco throttle (e.g. 0.80)
	balancedThreshold float64 // fraction of quota used to enter Balanced throttle (e.g. 0.50)
}

// NewSlidingWindowLimiter creates a sliding window limiter.
//
//	limit: max requests allowed per window (e.g. 300 for Alchemy free tier per minute)
//	window: measurement window duration (e.g. time.Minute)
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		window:            window,
		limit:             limit,
		timestamps:        make([]time.Time, limit),
		ecoThreshold:      0.80,
		balancedThreshold: 0.50,
	}
}

// SetThresholds overrides the default adaptive throttle thresholds.
// eco: fraction of window quota that triggers Eco mode (default 0.80)
// balanced: fraction that triggers Balanced mode (default 0.50)
func (s *SlidingWindowLimiter) SetThresholds(balanced, eco float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balancedThreshold = balanced
	s.ecoThreshold = eco
}

// Allow reports whether a request is permitted right now without blocking.
// Returns true and records the timestamp if within quota.
func (s *SlidingWindowLimiter) Allow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	if s.count >= s.limit {
		return false
	}
	s.record()
	return true
}

// Wait blocks until a request is permitted or ctx is cancelled.
// This is the primary entry point for Fetcher workers.
func (s *SlidingWindowLimiter) Wait(ctx context.Context) error {
	for {
		s.mu.Lock()
		s.evict()
		if s.count < s.limit {
			s.record()
			s.mu.Unlock()
			return nil
		}

		// Calculate how long until the oldest request falls out of the window.
		oldest := s.timestamps[s.head]
		waitUntil := oldest.Add(s.window)
		s.mu.Unlock()

		delay := time.Until(waitUntil)
		if delay <= 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// re-check after oldest entry expires
		}
	}
}

// QuotaUsed returns the number of requests consumed in the current window.
func (s *SlidingWindowLimiter) QuotaUsed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	return s.count
}

// QuotaRemaining returns the number of requests still available in the current window.
func (s *SlidingWindowLimiter) QuotaRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	return s.limit - s.count
}

// UsageFraction returns the fraction of the window quota consumed (0.0–1.0).
func (s *SlidingWindowLimiter) UsageFraction() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	if s.limit == 0 {
		return 1.0
	}
	return float64(s.count) / float64(s.limit)
}

// ThrottleMode returns the current adaptive throttle mode based on quota consumption.
// This replaces the binary Eco-Mode sleep with a three-tier gradient.
type ThrottleMode int

const (
	ThrottleAggressive ThrottleMode = iota // < 50% quota used: full speed
	ThrottleBalanced                        // 50–80% quota used: dynamic BPS
	ThrottleEco                             // > 80% quota used: minimal heartbeat
)

func (m ThrottleMode) String() string {
	switch m {
	case ThrottleAggressive:
		return "Aggressive"
	case ThrottleBalanced:
		return "Balanced"
	case ThrottleEco:
		return "Eco"
	default:
		return "Unknown"
	}
}

// CurrentMode returns the adaptive throttle mode without blocking.
func (s *SlidingWindowLimiter) CurrentMode() ThrottleMode {
	fraction := s.UsageFraction()
	switch {
	case fraction >= s.ecoThreshold:
		return ThrottleEco
	case fraction >= s.balancedThreshold:
		return ThrottleBalanced
	default:
		return ThrottleAggressive
	}
}

// RecommendedRPS returns the suggested RPS for the Fetcher's throughput limiter
// based on current quota consumption. Callers should apply this to
// Fetcher.SetThroughputLimit() to achieve gradient throttling.
//
// maxRPS: the configured maximum RPS for this environment (e.g. 15 for Sepolia)
func (s *SlidingWindowLimiter) RecommendedRPS(maxRPS float64) float64 {
	fraction := s.UsageFraction()
	switch {
	case fraction >= s.ecoThreshold:
		// Eco: 10% of max — maintain heartbeat, don't fully hibernate
		return maxRPS * 0.10
	case fraction >= s.balancedThreshold:
		// Balanced: linear scale-down from 100% → 10% as fraction goes 50% → 80%
		scale := 1.0 - ((fraction - s.balancedThreshold) / (s.ecoThreshold - s.balancedThreshold) * 0.90)
		return maxRPS * scale
	default:
		// Aggressive: full speed
		return maxRPS
	}
}

// WindowResetIn returns the duration until the oldest request in the current
// window expires (i.e., when quota will next be partially freed).
func (s *SlidingWindowLimiter) WindowResetIn() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	if s.count == 0 {
		return 0
	}
	oldest := s.timestamps[s.head]
	return time.Until(oldest.Add(s.window))
}

// evict removes timestamps that have fallen outside the sliding window.
// Must be called with s.mu held.
func (s *SlidingWindowLimiter) evict() {
	cutoff := time.Now().Add(-s.window)
	for s.count > 0 && s.timestamps[s.head].Before(cutoff) {
		s.head = (s.head + 1) % s.limit
		s.count--
	}
}

// record adds the current timestamp to the ring buffer.
// Must be called with s.mu held and after confirming count < limit.
func (s *SlidingWindowLimiter) record() {
	tail := (s.head + s.count) % s.limit
	s.timestamps[tail] = time.Now()
	s.count++
}
