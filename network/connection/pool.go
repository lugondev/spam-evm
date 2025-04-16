package connection

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

// max returns the larger of x or y
func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

type ClientPool struct {
	clients     []*managedClient
	mu          sync.RWMutex
	maxRetries  int
	endpoints   []string
	maxSize     int
	minSize     int
	healthCheck time.Duration
}

type managedClient struct {
	client       *ethclient.Client
	endpoint     string
	lastUsed     time.Time
	healthy      bool
	mu           sync.RWMutex
	lastError    error
	latency      time.Duration // Average latency
	errorCount   int64         // Number of errors
	requestCount int64         // Total number of requests
	lastCheck    time.Time     // Last performance check
}

// Performance score calculation (lower is better)
func (mc *managedClient) getScore() float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.requestCount == 0 {
		return float64(999999) // High score for unused clients
	}

	errorRate := float64(mc.errorCount) / float64(mc.requestCount)
	latencyScore := float64(mc.latency.Milliseconds())

	// Combined score weighs both latency and error rate
	return latencyScore * (1 + errorRate*10)
}

type PoolConfig struct {
	Endpoints   []string
	MaxSize     int
	MinSize     int
	MaxRetries  int
	HealthCheck time.Duration
}

func NewClientPool(config PoolConfig) (*ClientPool, error) {
	if len(config.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints provided")
	}

	pool := &ClientPool{
		clients:     make([]*managedClient, 0, config.MaxSize),
		endpoints:   config.Endpoints,
		maxSize:     config.MaxSize,
		minSize:     config.MinSize,
		maxRetries:  config.MaxRetries,
		healthCheck: config.HealthCheck,
	}

	// Initialize minimum number of connections
	for i := 0; i < config.MinSize; i++ {
		if err := pool.addClient(); err != nil {
			return nil, fmt.Errorf("failed to initialize minimum connections: %w", err)
		}
	}

	// Start health check routine
	go pool.healthCheckLoop()

	return pool, nil
}

func (p *ClientPool) addClient() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.clients) >= p.maxSize {
		return fmt.Errorf("pool at maximum capacity")
	}

	// Round-robin through endpoints
	endpoint := p.endpoints[len(p.clients)%len(p.endpoints)]
	client, err := ethclient.Dial(endpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to endpoint %s: %w", endpoint, err)
	}

	p.clients = append(p.clients, &managedClient{
		client:   client,
		endpoint: endpoint,
		healthy:  true,
	})

	return nil
}

func (p *ClientPool) GetClient() (*ethclient.Client, error) {
	p.mu.RLock()
	if len(p.clients) == 0 {
		p.mu.RUnlock()
		return nil, fmt.Errorf("no clients available")
	}

	// Get healthy clients and their scores
	type clientScore struct {
		client *managedClient
		score  float64
	}

	scores := make([]clientScore, 0)
	for _, mc := range p.clients {
		if mc.isHealthy() {
			scores = append(scores, clientScore{
				client: mc,
				score:  mc.getScore(),
			})
		}
	}
	p.mu.RUnlock()

	if len(scores) == 0 {
		// Try to add a new client if possible
		if err := p.addClient(); err != nil {
			return nil, fmt.Errorf("no healthy clients available and failed to add new client: %w", err)
		}
		return p.GetClient()
	}

	// Sort by score (lower is better)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score < scores[j].score
	})

	// Select from top 20% of clients randomly to balance load
	topCount := max(1, len(scores)/5)
	selected := scores[rand.Intn(topCount)].client

	selected.mu.Lock()
	selected.lastUsed = time.Now()
	selected.mu.Unlock()

	return selected.client, nil
}

func (mc *managedClient) isHealthy() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.healthy
}

func (p *ClientPool) healthCheckLoop() {
	ticker := time.NewTicker(p.healthCheck)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.RLock()
		clients := make([]*managedClient, len(p.clients))
		copy(clients, p.clients)
		p.mu.RUnlock()

		// Check health and adjust pool size based on performance
		for _, mc := range clients {
			go p.checkClientHealth(mc)
		}

		// Analyze pool performance and adjust size
		p.adjustPoolSize()
	}
}

// adjustPoolSize dynamically adjusts the pool size based on performance metrics
func (p *ClientPool) adjustPoolSize() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Calculate average performance score
	var totalScore float64
	for _, mc := range p.clients {
		totalScore += mc.getScore()
	}
	avgScore := totalScore / float64(len(p.clients))

	// If average score is poor, try adding more connections
	if avgScore > 1000 && len(p.clients) < p.maxSize {
		p.addClient()
	}

	// Remove poorly performing clients if we're above minSize
	if len(p.clients) > p.minSize {
		for i := len(p.clients) - 1; i >= 0; i-- {
			if p.clients[i].getScore() > avgScore*2 {
				p.clients[i].client.Close()
				p.clients = append(p.clients[:i], p.clients[i+1:]...)
			}
		}
	}
}

func (p *ClientPool) checkClientHealth(mc *managedClient) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Check if client can get latest block number
	_, err := mc.client.BlockNumber(ctx)
	latency := time.Since(start)

	mc.requestCount++
	mc.lastCheck = time.Now()

	if err != nil {
		mc.healthy = false
		mc.lastError = err
		mc.errorCount++

		// Try to reconnect
		if client, err := ethclient.Dial(mc.endpoint); err == nil {
			mc.client = client
			mc.healthy = true
			mc.lastError = nil
		}
	} else {
		mc.healthy = true
		mc.lastError = nil
		// Update latency with exponential moving average
		if mc.latency == 0 {
			mc.latency = latency
		} else {
			mc.latency = (mc.latency*4 + latency) / 5
		}
	}
}

// Close shuts down the client pool and closes all connections
func (p *ClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, mc := range p.clients {
		mc.client.Close()
	}
	p.clients = nil
}
