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

type (
	managedClient struct {
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
		successRate  float64       // Success rate over last window
		throughput   float64       // Requests per second
		lastFailure  time.Time     // Time of last failure
		consecutive  struct {      // Track consecutive successes/failures
			successes int64
			failures  int64
		}
	}

	// PerformanceMetrics tracks RPC endpoint performance
	PerformanceMetrics struct {
		Latency     time.Duration
		SuccessRate float64
		Throughput  float64
		ErrorCount  int64
		LastError   error
		LastCheck   time.Time
	}
)

// Performance score calculation (lower is better)
func (mc *managedClient) getScore() float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.requestCount == 0 {
		return float64(999999) // High score for unused clients
	}

	// Calculate base scores
	errorRate := float64(mc.errorCount) / float64(mc.requestCount)
	latencyScore := float64(mc.latency.Milliseconds())
	throughputScore := 100.0
	if mc.throughput > 0 {
		throughputScore = 100.0 / mc.throughput // Invert so lower is better
	}

	// Heavily penalize recent failures
	failurePenalty := 1.0
	if time.Since(mc.lastFailure) < time.Minute {
		failurePenalty = 2.0
	}

	// Reward consecutive successes
	successBonus := 1.0
	if mc.consecutive.successes > 10 {
		successBonus = 0.8
	}

	// Combined weighted score
	return (latencyScore*0.4 + throughputScore*0.3 + errorRate*100*0.3) *
		failurePenalty * successBonus
}

func (mc *managedClient) getMetrics() PerformanceMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	return PerformanceMetrics{
		Latency:     mc.latency,
		SuccessRate: mc.successRate,
		Throughput:  mc.throughput,
		ErrorCount:  mc.errorCount,
		LastError:   mc.lastError,
		LastCheck:   mc.lastCheck,
	}
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

		var wg sync.WaitGroup
		checkResults := make(chan bool, len(clients))

		// Perform parallel health checks
		for _, mc := range clients {
			wg.Add(1)
			go func(client *managedClient) {
				defer wg.Done()
				healthy := p.checkClientHealth(client)
				checkResults <- healthy
			}(mc)
		}

		// Wait for all checks to complete
		go func() {
			wg.Wait()
			close(checkResults)
		}()

		// Calculate health ratio
		total := 0
		healthy := 0
		for result := range checkResults {
			total++
			if result {
				healthy++
			}
		}

		healthRatio := float64(healthy) / float64(total)

		// Aggressive recovery if health ratio is too low
		if healthRatio < 0.5 {
			p.aggressiveRecovery()
		}

		// Regular pool size adjustment
		p.adjustPoolSize()
	}
}

func (p *ClientPool) aggressiveRecovery() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to recover each unhealthy client
	for _, mc := range p.clients {
		if !mc.isHealthy() {
			// Attempt immediate reconnection
			if client, err := ethclient.Dial(mc.endpoint); err == nil {
				mc.mu.Lock()
				mc.client = client
				mc.healthy = true
				mc.lastError = nil
				mc.consecutive.failures = 0
				mc.mu.Unlock()
			}
		}
	}

	// Add new clients if we're below minimum size
	for len(p.clients) < p.minSize {
		p.addClient()
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

func (p *ClientPool) checkClientHealth(mc *managedClient) bool {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Perform multiple health checks
	var successCount int
	var totalLatency time.Duration

	for i := 0; i < 3; i++ {
		// Check if client can get latest block number
		if _, err := mc.client.BlockNumber(ctx); err == nil {
			successCount++
			totalLatency += time.Since(start)
		}
		time.Sleep(100 * time.Millisecond)
	}

	mc.requestCount += 3
	mc.lastCheck = time.Now()

	// Calculate health metrics
	success := successCount >= 2 // Consider healthy if at least 2/3 checks pass
	avgLatency := totalLatency / time.Duration(successCount)

	if !success {
		mc.healthy = false
		mc.lastError = fmt.Errorf("health check failed: %d/3 successful", successCount)
		mc.errorCount++
		mc.lastFailure = time.Now()
		mc.consecutive.failures++
		mc.consecutive.successes = 0

		// Try to reconnect
		if client, err := ethclient.Dial(mc.endpoint); err == nil {
			mc.client = client
			mc.healthy = true
			mc.lastError = nil
		}
	} else {
		mc.healthy = true
		mc.lastError = nil
		mc.consecutive.successes++
		mc.consecutive.failures = 0

		// Update latency with weighted moving average
		if mc.latency == 0 {
			mc.latency = avgLatency
		} else {
			mc.latency = (mc.latency*4 + avgLatency) / 5
		}
	}

	// Update performance metrics
	windowSize := float64(100) // Consider last 100 requests
	mc.successRate = (windowSize - float64(mc.errorCount)) / windowSize
	mc.throughput = float64(mc.requestCount) / time.Since(mc.lastCheck).Seconds()

	return mc.healthy
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
