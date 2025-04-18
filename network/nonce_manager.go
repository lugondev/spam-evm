package network

import (
	"sync"
	"sync/atomic"
)

// NonceManager provides thread-safe nonce management with atomic operations
type NonceManager struct {
	nextNonce    uint64
	highestNonce uint64
	mu           sync.RWMutex
}

// NewNonceManager creates a new nonce manager starting from the given nonce
func NewNonceManager(startNonce uint64) *NonceManager {
	return &NonceManager{
		nextNonce:    startNonce,
		highestNonce: startNonce,
	}
}

// GetNonce atomically gets and increments the next nonce
func (nm *NonceManager) GetNonce() uint64 {
	return atomic.AddUint64(&nm.nextNonce, 1) - 1
}

// UpdateHighestNonce updates the highest known successful nonce
func (nm *NonceManager) UpdateHighestNonce(nonce uint64) bool {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nonce > nm.highestNonce {
		nm.highestNonce = nonce
		// If there's a gap, update nextNonce to one past highest known
		if nonce >= atomic.LoadUint64(&nm.nextNonce) {
			atomic.StoreUint64(&nm.nextNonce, nonce+1)
		}
		return true
	}
	return false
}

// GetHighestNonce returns the highest confirmed nonce
func (nm *NonceManager) GetHighestNonce() uint64 {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.highestNonce
}

// NonceManagerMap provides thread-safe access to nonce managers per address
type NonceManagerMap struct {
	managers map[string]*NonceManager
	mu       sync.RWMutex
}

// NewNonceManagerMap creates a new map for managing nonces
func NewNonceManagerMap() *NonceManagerMap {
	return &NonceManagerMap{
		managers: make(map[string]*NonceManager),
	}
}

// GetOrCreate returns existing nonce manager or creates new one
func (nmm *NonceManagerMap) GetOrCreate(address string, startNonce uint64) *NonceManager {
	nmm.mu.Lock()
	defer nmm.mu.Unlock()

	if nm, exists := nmm.managers[address]; exists {
		return nm
	}

	nm := NewNonceManager(startNonce)
	nmm.managers[address] = nm
	return nm
}
