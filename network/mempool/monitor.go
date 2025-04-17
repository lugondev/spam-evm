package mempool

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

type (
	Monitor struct {
		client         *ethclient.Client
		size           atomic.Int64
		lastUpdate     time.Time
		updateInterval time.Duration
		running        atomic.Bool
		metrics        struct {
			avgSize    atomic.Int64 // Average mempool size
			peakSize   atomic.Int64 // Peak mempool size
			growthRate atomic.Int64 // Rate of mempool growth per second
			congestion atomic.Int64 // Congestion score (0-100)
		}
		history []mempoolSnapshot // Historical snapshots for trend analysis
		mu      sync.RWMutex      // Protects history
	}

	mempoolSnapshot struct {
		size      int64
		timestamp time.Time
	}
)

const (
	maxHistorySize = 60 // Keep last 60 snapshots for trend analysis
	highCongestion = 80 // Threshold for high congestion (percent)
)

func NewMonitor(client *ethclient.Client, updateInterval time.Duration) *Monitor {
	m := &Monitor{
		client:         client,
		updateInterval: updateInterval,
	}
	return m
}

func (m *Monitor) Start(ctx context.Context) {
	if m.running.Swap(true) {
		return // Already running
	}

	go func() {
		ticker := time.NewTicker(m.updateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				m.running.Store(false)
				return
			case <-ticker.C:
				m.updateMempoolSize()
			}
		}
	}()
}

func (m *Monitor) updateMempoolSize() {
	// Get pending transactions count from the node
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	size, err := m.client.PendingTransactionCount(ctx)
	if err != nil {
		return
	}

	currentSize := int64(size)
	m.size.Store(currentSize)
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Add new snapshot
	m.history = append(m.history, mempoolSnapshot{
		size:      currentSize,
		timestamp: now,
	})

	// Maintain fixed history size
	if len(m.history) > maxHistorySize {
		m.history = m.history[1:]
	}

	// Update metrics
	if len(m.history) > 1 {
		// Calculate growth rate
		first := m.history[0]
		timeDiff := now.Sub(first.timestamp).Seconds()
		if timeDiff > 0 {
			growthRate := int64(float64(currentSize-first.size) / timeDiff)
			m.metrics.growthRate.Store(growthRate)
		}

		// Update peak size
		if currentSize > m.metrics.peakSize.Load() {
			m.metrics.peakSize.Store(currentSize)
		}

		// Calculate average size
		var sum int64
		for _, snapshot := range m.history {
			sum += snapshot.size
		}
		m.metrics.avgSize.Store(sum / int64(len(m.history)))

		// Calculate congestion score (0-100)
		congestion := m.calculateCongestionScore(currentSize)
		m.metrics.congestion.Store(congestion)
	}

	m.lastUpdate = now
}

func (m *Monitor) calculateCongestionScore(currentSize int64) int64 {
	var score float64

	// Factor 1: Current size relative to peak (40% weight)
	peakSize := m.metrics.peakSize.Load()
	if peakSize > 0 {
		score += 40 * (float64(currentSize) / float64(peakSize))
	}

	// Factor 2: Growth rate (30% weight)
	growthRate := m.metrics.growthRate.Load()
	if growthRate > 0 {
		normalizedGrowth := math.Min(float64(growthRate)/100.0, 1.0)
		score += 30 * normalizedGrowth
	}

	// Factor 3: Current vs average size (30% weight)
	avgSize := m.metrics.avgSize.Load()
	if avgSize > 0 {
		ratio := float64(currentSize) / float64(avgSize)
		normalizedRatio := math.Min(ratio, 2.0) / 2.0 // Cap at 2x average
		score += 30 * normalizedRatio
	}

	return int64(math.Min(score, 100))
}

func (m *Monitor) GetMempoolSize() int64 {
	return m.size.Load()
}

// GetLoadFactor returns a value between 0 and 1 indicating mempool congestion
func (m *Monitor) GetLoadFactor(maxSize int64) float64 {
	currentSize := m.GetMempoolSize()
	if currentSize >= maxSize {
		return 1.0
	}
	return float64(currentSize) / float64(maxSize)
}

// GetRecommendedBatchSize returns a recommended batch size based on mempool congestion
func (m *Monitor) GetRecommendedBatchSize(maxBatchSize, minBatchSize int) int {
	congestion := float64(m.metrics.congestion.Load()) / 100.0
	growthRate := m.metrics.growthRate.Load()

	// Base scaling factor from congestion
	factor := 1.0 - (congestion * congestion) // Exponential decrease with congestion

	// Adjust for growth rate
	if growthRate > 0 {
		growthPenalty := math.Min(float64(growthRate)/1000.0, 0.5) // Cap growth penalty at 50%
		factor *= (1.0 - growthPenalty)
	}

	// Calculate size with extra smoothing
	size := float64(maxBatchSize) * factor

	// Apply progressive reduction for high congestion
	if congestion > 0.8 {
		size *= 0.5 // Halve the batch size in high congestion
	}

	// Add small random variation to prevent synchronized batch submissions
	variation := size * 0.1 // 10% variation
	size += (rand.Float64() - 0.5) * variation

	// Ensure size stays within bounds
	if size < float64(minBatchSize) {
		return minBatchSize
	}
	if size > float64(maxBatchSize) {
		return maxBatchSize
	}
	return int(size)
}

// GetCongestionStatus returns detailed mempool congestion metrics
func (m *Monitor) GetCongestionStatus() (congestion, growthRate int64, avgSize, peakSize int64) {
	return m.metrics.congestion.Load(),
		m.metrics.growthRate.Load(),
		m.metrics.avgSize.Load(),
		m.metrics.peakSize.Load()
}
