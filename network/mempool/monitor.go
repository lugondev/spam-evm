package mempool

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

type Monitor struct {
	client         *ethclient.Client
	size           atomic.Int64
	lastUpdate     time.Time
	updateInterval time.Duration
	running        atomic.Bool
}

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
	size, err := m.client.PendingTransactionCount(context.Background())
	if err != nil {
		return
	}

	m.size.Store(int64(size))
	m.lastUpdate = time.Now()
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

// GetRecommendedBatchSize returns a recommended batch size based on mempool load
func (m *Monitor) GetRecommendedBatchSize(maxBatchSize, minBatchSize int) int {
	loadFactor := m.GetLoadFactor(10000) // Assume max mempool size of 10000

	// Exponentially decrease batch size as load increases
	factor := 1.0 - (loadFactor * loadFactor)
	size := float64(maxBatchSize) * factor

	// Ensure size stays within bounds
	if size < float64(minBatchSize) {
		return minBatchSize
	}
	if size > float64(maxBatchSize) {
		return maxBatchSize
	}
	return int(size)
}
