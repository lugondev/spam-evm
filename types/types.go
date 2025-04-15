package types

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Wallet struct {
	PrivateKey *ecdsa.PrivateKey
	Address    common.Address
	Client     *ethclient.Client
	Nonce      uint64
}

type PerformanceMetrics struct {
	ConnectionTime     time.Duration
	SignTime           time.Duration
	SendTime           time.Duration
	TotalTransactions  int
	FailedTransactions int
	Mutex              sync.Mutex
}

func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{}
}

func (pm *PerformanceMetrics) AddConnectionTime(d time.Duration) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.ConnectionTime += d
}

func (pm *PerformanceMetrics) AddSignTime(d time.Duration) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.SignTime += d
}

func (pm *PerformanceMetrics) AddSendTime(d time.Duration) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.SendTime += d
}

func (pm *PerformanceMetrics) IncrementTransactions() {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.TotalTransactions++
}

func (pm *PerformanceMetrics) IncrementFailedTransactions() {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.FailedTransactions++
}

// NetworkParams holds chain-wide parameters that are common across all wallets
type NetworkParams struct {
	ChainID     *big.Int
	GasPrice    *big.Int
	LastUpdated time.Time
	mutex       sync.RWMutex
}

// NewNetworkParams initializes network parameters
func NewNetworkParams(ctx context.Context, client *ethclient.Client) (*NetworkParams, error) {
	params := &NetworkParams{}
	if err := params.Update(ctx, client); err != nil {
		return nil, err
	}
	return params, nil
}

// Update refreshes the network parameters
func (np *NetworkParams) Update(ctx context.Context, client *ethclient.Client) error {
	np.mutex.Lock()
	defer np.mutex.Unlock()

	// Get chain ID with retry
	var err error
	for i := 0; i < 3; i++ {
		np.ChainID, err = client.ChainID(ctx)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	// Get gas price with retry
	for i := 0; i < 3; i++ {
		np.GasPrice, err = client.SuggestGasPrice(ctx)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}

	// Add 10% to gas price as buffer
	np.GasPrice = new(big.Int).Mul(np.GasPrice, big.NewInt(110))
	np.GasPrice = new(big.Int).Div(np.GasPrice, big.NewInt(100))

	np.LastUpdated = time.Now()
	return nil
}

// GetChainID returns the cached chain ID
func (np *NetworkParams) GetChainID() *big.Int {
	np.mutex.RLock()
	defer np.mutex.RUnlock()
	return new(big.Int).Set(np.ChainID)
}

// GetGasPrice returns the cached gas price
func (np *NetworkParams) GetGasPrice() *big.Int {
	np.mutex.RLock()
	defer np.mutex.RUnlock()
	return new(big.Int).Set(np.GasPrice)
}
