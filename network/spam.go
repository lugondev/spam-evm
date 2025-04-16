package network

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"spam-evm/network/connection"
	"spam-evm/network/mempool"
	"spam-evm/pkg"
	"spam-evm/types"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	maxBatchSize         = 200             // Maximum number of transactions per batch
	minBatchSize         = 20              // Minimum number of transactions per batch
	nonceQueueSize       = 1000            // Size of nonce queue per wallet
	maxBatchAttempts     = 3               // Maximum attempts to send a batch
	batchTimeout         = 5               // Seconds to wait for batch completion
	memPoolCheckInterval = 5 * time.Second // How often to check mempool size
)

var (
	defaultPoolConfig = connection.PoolConfig{
		MaxSize:     100,
		MinSize:     20,
		MaxRetries:  3,
		HealthCheck: 30 * time.Second,
	}

	maxMemPoolSize int64 = 10000 // Maximum allowed mempool size
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
	for i := range size {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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

	// Initialize mempool monitor
	client, err := pool.GetClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get client for mempool monitor: %w", err)
	}
	monitor := mempool.NewMonitor(client, memPoolCheckInterval)
	monitor.Start(ctx)

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

	// Batch size control channel
	batchSizeChan := make(chan int, 1)
	batchSizeChan <- maxBatchSize // Start with max batch size

	// Monitor mempool and adjust batch size
	go func() {
		ticker := time.NewTicker(memPoolCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recommendedSize := monitor.GetRecommendedBatchSize(maxBatchSize, minBatchSize)
				select {
				case <-batchSizeChan: // Clear old value
				default:
				}
				batchSizeChan <- recommendedSize
			}
		}
	}()

	// Start batch processors
	var wg sync.WaitGroup
	for range maxConcurrency {
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
							delay := time.Duration(100*(attempt+1)) * time.Millisecond
							time.Sleep(delay)
							continue
						}

						resultChan <- tx.Hash()
						metrics.IncrementTransactions()

						// Return successful nonce
						nonceQueues[batch.wallet.Address].returnNonce(batch.nonces[i])
					}

					metrics.AddSendTime(time.Since(sendStart))
					cancel()
					break
				}

				// Apply backpressure based on mempool size
				if loadFactor := monitor.GetLoadFactor(maxMemPoolSize); loadFactor > 0.8 {
					delay := time.Duration(float64(500)*loadFactor) * time.Millisecond
					time.Sleep(delay)
				}
			}
		}()
	}

	// Transaction generator goroutines
	txGenerator := func(w *types.Wallet) {
		// Get current batch size
		currentBatchSize := <-batchSizeChan
		batchSizeChan <- currentBatchSize

		batch := &txBatch{
			transactions: make([]*ethTypes.Transaction, 0, currentBatchSize),
			nonces:       make([]uint64, 0, currentBatchSize),
			wallet:       w,
		}

		for range count {
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
			if len(batch.transactions) >= currentBatchSize {
				batchChan <- batch

				// Get new batch size for next batch
				currentBatchSize = <-batchSizeChan
				batchSizeChan <- currentBatchSize

				batch = &txBatch{
					transactions: make([]*ethTypes.Transaction, 0, currentBatchSize),
					nonces:       make([]uint64, 0, currentBatchSize),
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
