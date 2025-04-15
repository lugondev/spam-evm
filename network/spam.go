package network

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"spam-evm/pkg"
	"spam-evm/types"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

func SpamNetwork(wallets []*types.Wallet, count int, maxConcurrency int, params *types.NetworkParams) ([]common.Hash, *types.PerformanceMetrics, error) {
	ctx := context.Background()
	log.Println("Starting spam network...")

	stopMonitor := make(chan struct{})
	metrics := types.NewPerformanceMetrics()

	minValue := big.NewInt(10000000000000) // 0.00001 ETH in wei
	startTime := time.Now()

	resultChan := make(chan common.Hash, len(wallets)*count)
	errorChan := make(chan error, len(wallets)*count)

	var sem chan struct{}
	if maxConcurrency > 0 {
		sem = make(chan struct{}, maxConcurrency)
	}

	var wg sync.WaitGroup
	for _, w := range wallets {
		wg.Add(1)
		go func(w *types.Wallet) {
			defer wg.Done()

			nonce := w.Nonce

			for i := 0; i < count; i++ {
				if sem != nil {
					sem <- struct{}{}
				}

				randomAddress := pkg.GenerateRandomEthAddress()
				tx := ethTypes.NewTransaction(
					nonce+uint64(i),
					randomAddress,
					minValue,
					uint64(21000),
					params.GetGasPrice(),
					nil,
				)

				signStart := time.Now()
				signedTx, err := ethTypes.SignTx(tx, ethTypes.NewEIP155Signer(params.GetChainID()), w.PrivateKey)
				metrics.AddSignTime(time.Since(signStart))

				if err != nil {
					metrics.IncrementFailedTransactions()
					errorChan <- fmt.Errorf("failed to sign transaction: %v", err)
					if sem != nil {
						<-sem
					}
					continue
				}

				sendStart := time.Now()
				err = w.Client.SendTransaction(ctx, signedTx)
				metrics.AddSendTime(time.Since(sendStart))

				if err != nil {
					metrics.IncrementFailedTransactions()
					errorChan <- fmt.Errorf("failed to send transaction: %v", err)
					if sem != nil {
						<-sem
					}
					continue
				}

				metrics.IncrementTransactions()
				resultChan <- signedTx.Hash()

				if sem != nil {
					<-sem
				}
			}
		}(w)
	}

	wg.Wait()
	close(stopMonitor)

	endTime := time.Now()
	totalTime := endTime.Sub(startTime)
	totalSpam := metrics.TotalTransactions
	tps := float64(totalSpam) / totalTime.Seconds()

	log.Printf("Spam completed in %.2f seconds", totalTime.Seconds())
	log.Printf("Total Transactions: %d", totalSpam)
	log.Printf("Failed Transactions: %d", metrics.FailedTransactions)
	log.Printf("TPS: %.2f", tps)

	close(resultChan)
	close(errorChan)

	var transactions []common.Hash
	for hash := range resultChan {
		transactions = append(transactions, hash)
	}

	return transactions, metrics, nil
}
