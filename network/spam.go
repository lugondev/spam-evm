package network

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"spam-evm/network/connection"
	"spam-evm/pkg"
	"spam-evm/types"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	batchSize        = 100   // Number of transactions to batch before sending
	nonceQueueSize   = 1000  // Size of nonce queue per wallet
	memPoolThreshold = 10000 // Maximum number of pending transactions before throttling
	throttleDelay    = 100   // Milliseconds to wait when mempool is full
	maxBatchAttempts = 3     // Maximum attempts to send a batch
	batchTimeout     = 5     // Seconds to wait for batch completion
)

var (
	defaultPoolConfig = connection.PoolConfig{
		MaxSize:     100, // Increased from 50
		MinSize:     20,  // Increased from 10
		MaxRetries:  3,
		HealthCheck: 30 * time.Second,
	}

	defaultRetryConfig = connection.RetryConfig{
		MaxAttempts:       3,
		InitialDelay:      100 * time.Millisecond,
		MaxDelay:          2 * time.Second,
		BackoffMultiplier: 2.0,
	}
)

type nonceQueue struct {
	nonces    chan uint64
	nextNonce uint64
	mu        sync.Mutex
}

func newNonceQueue(startNonce uint64, size int) *nonceQueue {
	q := &nonceQueue{
		nonces:    make(chan uint64, size),
		nextNonce: startNonce,
	}

	// Pre-fill nonce queue
	for i := 0; i < size; i++ {
		q.nonces <- startNonce + uint64(i)
	}

	return q
}

func (q *nonceQueue) getNonce() uint64 {
	return <-q.nonces
}

func (q *nonceQueue) returnNonce(n uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if n >= q.nextNonce {
		q.nextNonce = n + 1
		q.nonces <- q.nextNonce
	}
}

type txBatch struct {
	transactions []*ethTypes.Transaction
	nonces       []uint64
	wallet       *types.Wallet
}

func SpamNetwork(wallets []*types.Wallet, count int, maxConcurrency int, params *types.NetworkParams) ([]common.Hash, *types.PerformanceMetrics, error) {
	ctx := context.Background()
	log.Println("Starting optimized spam network...")

	poolConfig := defaultPoolConfig
	poolConfig.Endpoints = params.ProviderURLs

	if len(poolConfig.Endpoints) == 0 {
		return nil, nil, fmt.Errorf("no provider URLs configured")
	}

	pool, err := connection.NewClientPool(poolConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize client pool: %w", err)
	}
	defer pool.Close()

	metrics := types.NewPerformanceMetrics()
	startTime := time.Now()

	// Create nonce queues for each wallet
	nonceQueues := make(map[common.Address]*nonceQueue)
	for _, w := range wallets {
		nonceQueues[w.Address] = newNonceQueue(w.Nonce, nonceQueueSize)
	}

	// Channel for completed transaction hashes
	resultChan := make(chan common.Hash, len(wallets)*count)
	errorChan := make(chan error, len(wallets)*count)

	// Channel for batch processing
	batchChan := make(chan *txBatch, maxConcurrency)

	// Track pending transactions
	var pendingTxs int64

	// Start batch processors
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batchChan {
				client, err := pool.GetClient()
				if err != nil {
					for range batch.transactions {
						errorChan <- fmt.Errorf("failed to get client from pool: %w", err)
					}
					continue
				}

				// Send batch with retry
				for attempt := 0; attempt < maxBatchAttempts; attempt++ {
					batchCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(batchTimeout))
					sendStart := time.Now()

					for i, tx := range batch.transactions {
						if err := client.SendTransaction(batchCtx, tx); err != nil {
							log.Printf("Batch send error (attempt %d): %v", attempt+1, err)
							time.Sleep(time.Duration(throttleDelay) * time.Millisecond)
							continue
						}

						resultChan <- tx.Hash()
						metrics.IncrementTransactions()
						atomic.AddInt64(&pendingTxs, 1)

						// Return successful nonce
						nonceQueues[batch.wallet.Address].returnNonce(batch.nonces[i])
					}

					metrics.AddSendTime(time.Since(sendStart))
					cancel()
					break
				}

				// Throttle if mempool is getting full
				if atomic.LoadInt64(&pendingTxs) > memPoolThreshold {
					time.Sleep(time.Duration(throttleDelay) * time.Millisecond)
				}
			}
		}()
	}

	// Transaction generator goroutines
	txGenerator := func(w *types.Wallet) {
		batch := &txBatch{
			transactions: make([]*ethTypes.Transaction, 0, batchSize),
			nonces:       make([]uint64, 0, batchSize),
			wallet:       w,
		}

		for i := 0; i < count; i++ {
			nonce := nonceQueues[w.Address].getNonce()

			// Create transaction
			tx := ethTypes.NewTransaction(
				nonce,
				pkg.GenerateRandomEthAddress(),
				big.NewInt(10000000000000), // 0.00001 ETH
				uint64(21000),
				params.GetGasPrice(),
				nil,
			)

			// Sign transaction
			signStart := time.Now()
			signedTx, err := ethTypes.SignTx(
				tx,
				ethTypes.NewEIP155Signer(params.GetChainID()),
				w.PrivateKey,
			)
			metrics.AddSignTime(time.Since(signStart))

			if err != nil {
				errorChan <- fmt.Errorf("failed to sign transaction: %w", err)
				continue
			}

			batch.transactions = append(batch.transactions, signedTx)
			batch.nonces = append(batch.nonces, nonce)

			// Send batch when full
			if len(batch.transactions) >= batchSize {
				batchChan <- batch
				batch = &txBatch{
					transactions: make([]*ethTypes.Transaction, 0, batchSize),
					nonces:       make([]uint64, 0, batchSize),
					wallet:       w,
				}
			}
		}

		// Send remaining transactions
		if len(batch.transactions) > 0 {
			batchChan <- batch
		}
	}

	// Start transaction generators
	var genWg sync.WaitGroup
	for _, w := range wallets {
		genWg.Add(1)
		go func(wallet *types.Wallet) {
			defer genWg.Done()
			txGenerator(wallet)
		}(w)
	}

	// Wait for all generators to complete
	genWg.Wait()
	close(batchChan)

	// Wait for all batches to be processed
	wg.Wait()

	close(resultChan)
	close(errorChan)

	endTime := time.Now()
	totalTime := endTime.Sub(startTime)
	totalSpam := metrics.TotalTransactions
	tps := float64(totalSpam) / totalTime.Seconds()

	log.Printf("Spam completed in %.2f seconds", totalTime.Seconds())
	log.Printf("Total Transactions: %d", totalSpam)
	log.Printf("Failed Transactions: %d", metrics.FailedTransactions)
	log.Printf("TPS: %.2f", tps)

	var transactions []common.Hash
	for hash := range resultChan {
		transactions = append(transactions, hash)
	}

	return transactions, metrics, nil
}
