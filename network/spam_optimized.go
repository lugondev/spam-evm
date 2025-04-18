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
	"spam-evm/wallet"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	optMaxBatchSize         = 500             // Increased max batch size
	optMinBatchSize         = 50              // Increased min batch size
	optMaxBatchAttempts     = 5               // Increased max attempts
	optBatchTimeout         = 10              // Increased timeout
	optMemPoolCheckInterval = 2 * time.Second // Reduced interval
)

// txBatchOptimized represents a batch of transactions with metadata
type txBatchOptimized struct {
	transactions []*ethTypes.Transaction
	wallet       *types.Wallet
	batchID      int
}

// SpamNetworkOptimized provides an optimized implementation of network spamming
func SpamNetworkOptimized(privateKeys []string, count int, maxConcurrency int, params *types.NetworkParams) ([]common.Hash, *types.PerformanceMetrics, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Starting optimized spam network...")

	// Initialize connection pool
	poolConfig := connection.PoolConfig{
		Endpoints:   params.ProviderURLs,
		MaxSize:     maxConcurrency * 2, // Increased pool size
		MinSize:     maxConcurrency,
		MaxRetries:  5,
		HealthCheck: 30 * time.Second,
	}

	pool, err := connection.NewClientPool(poolConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize client pool: %w", err)
	}
	defer pool.Close()

	client, err := pool.GetClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get initial client: %w", err)
	}

	// Initialize performance metrics
	metrics := types.NewPerformanceMetrics()
	startTime := time.Now()

	// Initialize wallets in parallel
	wallets, err := wallet.InitializeWallets(privateKeys, client)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize wallets: %w", err)
	}

	// Initialize mempool monitor with reduced interval
	monitor := mempool.NewMonitor(client, optMemPoolCheckInterval)
	monitor.Start(ctx)

	// Initialize nonce managers for each wallet
	nonceManagers := NewNonceManagerMap()
	for _, w := range wallets {
		nonceManagers.GetOrCreate(w.Address.String(), w.Nonce)
	}

	// Channels for processing
	batchChan := make(chan *txBatchOptimized, maxConcurrency*2)
	resultChan := make(chan common.Hash, count*len(wallets))
	errorChan := make(chan error, count*len(wallets))

	// Start batch processors
	var processorWg sync.WaitGroup
	for i := 0; i < maxConcurrency; i++ {
		processorWg.Add(1)
		go processBatchesOptimized(
			ctx,
			pool,
			batchChan,
			resultChan,
			errorChan,
			metrics,
			nonceManagers,
			monitor,
			&processorWg,
		)
	}

	// Start transaction generation
	var generatorWg sync.WaitGroup
	batchSize := maxBatchSize // Start with max batch size

	for _, w := range wallets {
		generatorWg.Add(1)
		go func(wallet *types.Wallet) {
			defer generatorWg.Done()

			nm := nonceManagers.GetOrCreate(wallet.Address.String(), wallet.Nonce)
			remainingTx := count
			batchID := 0

			for remainingTx > 0 {
				// Check mempool status and adjust batch size
				if loadFactor := monitor.GetLoadFactor(10000); loadFactor > 0.8 {
					batchSize = optMinBatchSize
				} else {
					batchSize = optMaxBatchSize
				}

				// Calculate actual batch size
				currentBatchSize := min(batchSize, remainingTx)

				// Create batch
				batch := &txBatchOptimized{
					transactions: make([]*ethTypes.Transaction, 0, currentBatchSize),
					wallet:       wallet,
					batchID:      batchID,
				}

				// Generate transactions for batch
				for i := 0; i < currentBatchSize; i++ {
					nonce := nm.GetNonce()
					tx := ethTypes.NewTransaction(
						nonce,
						pkg.GenerateRandomEthAddress(),
						big.NewInt(100000000000),
						uint64(21000),
						params.GetGasPrice(),
						nil,
					)

					signStart := time.Now()
					signedTx, err := ethTypes.SignTx(
						tx,
						ethTypes.NewEIP155Signer(params.GetChainID()),
						wallet.PrivateKey,
					)
					metrics.AddSignTime(time.Since(signStart))

					if err != nil {
						errorChan <- fmt.Errorf("failed to sign transaction: %w", err)
						continue
					}

					batch.transactions = append(batch.transactions, signedTx)
				}

				if len(batch.transactions) > 0 {
					batchChan <- batch
					remainingTx -= len(batch.transactions)
					batchID++
				}

				// Apply backpressure if mempool is stressed
				if loadFactor := monitor.GetLoadFactor(10000); loadFactor > 0.9 {
					time.Sleep(time.Duration(float64(100)*loadFactor) * time.Millisecond)
				}
			}
		}(w)
	}

	// Wait for generation to complete
	generatorWg.Wait()
	close(batchChan)

	// Wait for processing to complete
	processorWg.Wait()
	close(resultChan)
	close(errorChan)

	// Collect results
	var transactions []common.Hash
	for hash := range resultChan {
		transactions = append(transactions, hash)
	}

	// Log performance metrics
	duration := time.Since(startTime)
	tps := float64(len(transactions)) / duration.Seconds()

	log.Printf("Spam completed in %.2f seconds", duration.Seconds())
	log.Printf("Total Transactions: %d", len(transactions))
	log.Printf("TPS: %.2f", tps)

	return transactions, metrics, nil
}

// processBatchesOptimized handles the optimized batch processing
func processBatchesOptimized(
	ctx context.Context,
	pool *connection.ClientPool,
	batchChan <-chan *txBatchOptimized,
	resultChan chan<- common.Hash,
	errorChan chan<- error,
	metrics *types.PerformanceMetrics,
	nonceManagers *NonceManagerMap,
	monitor *mempool.Monitor,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for batch := range batchChan {
		client, err := pool.GetClient()
		if err != nil {
			for range batch.transactions {
				errorChan <- fmt.Errorf("failed to get client: %w", err)
			}
			continue
		}

		// Process batch with exponential backoff
		var sent int
		for attempt := 0; attempt < optMaxBatchAttempts; attempt++ {
			if attempt > 0 {
				delay := time.Duration(100<<uint(attempt-1)) * time.Millisecond
				time.Sleep(delay)
			}

			batchCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(optBatchTimeout))
			sendStart := time.Now()

			for i := sent; i < len(batch.transactions); i++ {
				tx := batch.transactions[i]
				if err := client.SendTransaction(batchCtx, tx); err != nil {
					log.Printf("Batch %d send error (attempt %d): %v", batch.batchID, attempt+1, err)
					continue
				}

				resultChan <- tx.Hash()
				metrics.IncrementTransactions()
				sent++

				// Update nonce manager
				nm := nonceManagers.GetOrCreate(batch.wallet.Address.String(), 0)
				nm.UpdateHighestNonce(tx.Nonce())
			}

			metrics.AddSendTime(time.Since(sendStart))
			cancel()

			if sent == len(batch.transactions) {
				break
			}
		}

		// Report unsent transactions as errors
		for i := sent; i < len(batch.transactions); i++ {
			errorChan <- fmt.Errorf("failed to send transaction after %d attempts", optMaxBatchAttempts)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
