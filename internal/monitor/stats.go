package monitor

import (
	"sync"
	"time"
)

// TPSMonitor implements a sliding window (5s) for deterministic TPS calculation
type TPSMonitor struct {
	buckets    [5]int
	currentPos int
	lastTick   time.Time
	mu         sync.Mutex
}

func NewTPSMonitor() *TPSMonitor {
	return &TPSMonitor{
		lastTick: time.Now(),
	}
}

// Record increments the count for the current second bucket
func (m *TPSMonitor) Record(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	// Check if we need to advance the window
	elapsed := int(now.Sub(m.lastTick).Seconds())
	if elapsed >= 1 {
		// If more than 5 seconds passed, clear everything
		if elapsed >= 5 {
			for i := range m.buckets {
				m.buckets[i] = 0
			}
			m.currentPos = 0
		} else {
			// Advance and clear intermediate buckets
			for i := 0; i < elapsed; i++ {
				m.currentPos = (m.currentPos + 1) % 5
				m.buckets[m.currentPos] = 0
			}
		}
		m.lastTick = now
	}
	m.buckets[m.currentPos] += count
}

// GetTPS returns the average TPS over the 5-second window
func (m *TPSMonitor) GetTPS() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Handle potential long periods of inactivity
	if time.Since(m.lastTick) > 5*time.Second {
		return 0.0
	}

	sum := 0
	for _, b := range m.buckets {
		sum += b
	}
	return float64(sum) / 5.0
}
