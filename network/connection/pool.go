package connection

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

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
	client    *ethclient.Client
	endpoint  string
	lastUsed  time.Time
	healthy   bool
	mu        sync.RWMutex
	lastError error
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

	// Get healthy clients
	healthyClients := make([]*managedClient, 0)
	for _, mc := range p.clients {
		if mc.isHealthy() {
			healthyClients = append(healthyClients, mc)
		}
	}
	p.mu.RUnlock()

	if len(healthyClients) == 0 {
		// Try to add a new client if possible
		if err := p.addClient(); err != nil {
			return nil, fmt.Errorf("no healthy clients available and failed to add new client: %w", err)
		}
		return p.GetClient()
	}

	// Select a random healthy client
	selected := healthyClients[rand.Intn(len(healthyClients))]
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

		for _, mc := range clients {
			go p.checkClientHealth(mc)
		}
	}
}

func (p *ClientPool) checkClientHealth(mc *managedClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Check if client can get latest block number
	_, err := mc.client.BlockNumber(ctx)
	if err != nil {
		mc.healthy = false
		mc.lastError = err
		// Try to reconnect
		if client, err := ethclient.Dial(mc.endpoint); err == nil {
			mc.client = client
			mc.healthy = true
			mc.lastError = nil
		}
	} else {
		mc.healthy = true
		mc.lastError = nil
	}
}

func (p *ClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, mc := range p.clients {
		mc.client.Close()
	}
	p.clients = nil
}
