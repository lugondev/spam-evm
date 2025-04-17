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

type (
	nonceQueue struct {
		nonces    chan uint64
		nextNonce uint64
		mu        sync.Mutex
	}

	txBatch struct {
		transactions []*ethTypes.Transaction
		nonces       []uint64
		wallet       *types.Wallet
		prepared     bool
	}

	precomputedTx struct {
		tx    *ethTypes.Transaction
		nonce uint64
	}

	workPool struct {
		workers   int
		tasks     chan func()
		completed chan struct{}
		wg        sync.WaitGroup
	}
)

func newWorkPool(workers int) *workPool {
	wp := &workPool{
		workers:   workers,
		tasks:     make(chan func(), workers*2),
		completed: make(chan struct{}),
	}

	wp.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go wp.worker()
	}

	return wp
}

func (wp *workPool) worker() {
	defer wp.wg.Done()
	for task := range wp.tasks {
		task()
	}
}

func (wp *workPool) submit(task func()) {
	wp.tasks <- task
}

func (wp *workPool) close() {
	close(wp.tasks)
	wp.wg.Wait()
	close(wp.completed)
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

func SpamNetwork(wallets []*types.Wallet, count int, maxConcurrency int, params *types.NetworkParams) ([]common.Hash, *types.PerformanceMetrics, error) {
	// Calculate optimal worker count based on CPU cores and concurrency
	workerCount := maxConcurrency * 2
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

	// Initialize work pool for parallel tx pre-computation
	workPool := newWorkPool(workerCount)
	defer workPool.close()

	// Channel for pre-computed transactions
	precomputedChan := make(chan *precomputedTx, maxConcurrency*maxBatchSize)

	// Channel for batch processing
	batchChan := make(chan *txBatch, maxConcurrency)

	// Batch size control channel with buffer for reduced contention
	batchSizeChan := make(chan int, maxConcurrency)
	for i := 0; i < maxConcurrency; i++ {
		batchSizeChan <- maxBatchSize // Start with max batch size
	}

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

	// Start batch processors with improved error handling and backoff
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Maintain a client connection for the processor
			client, err := pool.GetClient()
			if err != nil {
				log.Printf("Processor failed to get initial client: %v", err)
				return
			}

			for batch := range batchChan {
				if !batch.prepared {
					continue
				}

				sendStart := time.Now()
				batchSuccess := true

				// Send batch with exponential backoff retry
				for attempt := 0; attempt < maxBatchAttempts; attempt++ {
					if attempt > 0 {
						// Exponential backoff with jitter
						backoff := time.Duration(100*(1<<attempt)) * time.Millisecond
						time.Sleep(backoff + time.Duration(pkg.RandomInt(0, 100))*time.Millisecond)

						// Try to get a fresh client for retry
						if newClient, err := pool.GetClient(); err == nil {
							client = newClient
						}
					}

					batchCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(batchTimeout))

					for i, tx := range batch.transactions {
						if err := client.SendTransaction(batchCtx, tx); err != nil {
							log.Printf("TX send error (attempt %d): %v", attempt+1, err)
							batchSuccess = false
							continue
						}

						resultChan <- tx.Hash()
						metrics.IncrementTransactions()
						nonceQueues[batch.wallet.Address].returnNonce(batch.nonces[i])
					}

					cancel()

					if batchSuccess {
						break
					}
				}

				metrics.AddSendTime(time.Since(sendStart))

				// Dynamic backpressure based on mempool state
				loadFactor := monitor.GetLoadFactor(maxMemPoolSize)
				if loadFactor > 0.8 {
					delay := time.Duration(float64(200+pkg.RandomInt(0, 300))*loadFactor) * time.Millisecond
					time.Sleep(delay)
				}
			}
		}()
	}

	// Pre-compute transactions in parallel
	txPrecomputer := func(w *types.Wallet) {
		signer := ethTypes.NewEIP155Signer(params.GetChainID())

		for i := 0; i < count; i++ {
			nonce := nonceQueues[w.Address].getNonce()

			workPool.submit(func() {
				// Create and sign transaction
				tx := ethTypes.NewTransaction(
					nonce,
					pkg.GenerateRandomEthAddress(),
					big.NewInt(100000000000),
					uint64(21000),
					params.GetGasPrice(),
					nil,
				)

				signStart := time.Now()
				signedTx, err := ethTypes.SignTx(tx, signer, w.PrivateKey)
				metrics.AddSignTime(time.Since(signStart))

				if err != nil {
					errorChan <- fmt.Errorf("failed to sign transaction: %w", err)
					nonceQueues[w.Address].returnNonce(nonce)
					return
				}

				precomputedChan <- &precomputedTx{
					tx:    signedTx,
					nonce: nonce,
				}
			})
		}
	}

	// Transaction batcher goroutine
	txBatcher := func(w *types.Wallet) {
		currentBatchSize := <-batchSizeChan

		// Calculate batch cost once
		txValue := big.NewInt(100000000000)
		gasPrice := params.GetGasPrice()
		gasCost := new(big.Int).Mul(gasPrice, big.NewInt(21000))
		totalCostPerTx := new(big.Int).Add(txValue, gasCost)
		batchCost := new(big.Int).Mul(totalCostPerTx, big.NewInt(int64(currentBatchSize)))

		if w.Balance.Cmp(batchCost) < 0 {
			errorChan <- fmt.Errorf("insufficient balance for batch: have %v, need %v", w.Balance, batchCost)
			return
		}

		batch := &txBatch{
			transactions: make([]*ethTypes.Transaction, 0, currentBatchSize),
			nonces:       make([]uint64, 0, currentBatchSize),
			wallet:       w,
		}

		batchTimer := time.NewTimer(5 * time.Second)
		defer batchTimer.Stop()

		for {
			select {
			case ptx, ok := <-precomputedChan:
				if !ok {
					// Send remaining transactions
					if len(batch.transactions) > 0 {
						batch.prepared = true
						batchChan <- batch
					}
					return
				}

				batch.transactions = append(batch.transactions, ptx.tx)
				batch.nonces = append(batch.nonces, ptx.nonce)

				// Send batch when full
				if len(batch.transactions) >= currentBatchSize {
					batch.prepared = true
					batchChan <- batch

					// Get new batch size for next batch
					currentBatchSize = <-batchSizeChan
					batchSizeChan <- currentBatchSize

					batch = &txBatch{
						transactions: make([]*ethTypes.Transaction, 0, currentBatchSize),
						nonces:       make([]uint64, 0, currentBatchSize),
						wallet:       w,
					}

					// Reset batch timer
					batchTimer.Reset(5 * time.Second)
				}

			case <-batchTimer.C:
				// Send partial batch after timeout
				if len(batch.transactions) > 0 {
					batch.prepared = true
					batchChan <- batch

					currentBatchSize = <-batchSizeChan
					batchSizeChan <- currentBatchSize

					batch = &txBatch{
						transactions: make([]*ethTypes.Transaction, 0, currentBatchSize),
						nonces:       make([]uint64, 0, currentBatchSize),
						wallet:       w,
					}
				}
				batchTimer.Reset(5 * time.Second)
			}
		}
	}

	// Start transaction pre-computation and batching
	var genWg sync.WaitGroup
	for _, w := range wallets {
		genWg.Add(2)

		// Start pre-computer
		go func(wallet *types.Wallet) {
			defer genWg.Done()
			txPrecomputer(wallet)
		}(w)

		// Start batcher
		go func(wallet *types.Wallet) {
			defer genWg.Done()
			txBatcher(wallet)
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
