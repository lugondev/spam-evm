package types

import (
	"crypto/ecdsa"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Wallet struct {
	PrivateKey *ecdsa.PrivateKey
	Address    common.Address
	Client     *ethclient.Client
}

type PerformanceMetrics struct {
	ConnectionTime     time.Duration
	NonceTime          time.Duration
	ChainIDTime        time.Duration
	GasPriceTime       time.Duration
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

func (pm *PerformanceMetrics) AddNonceTime(d time.Duration) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.NonceTime += d
}

func (pm *PerformanceMetrics) AddChainIDTime(d time.Duration) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.ChainIDTime += d
}

func (pm *PerformanceMetrics) AddGasPriceTime(d time.Duration) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.GasPriceTime += d
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
