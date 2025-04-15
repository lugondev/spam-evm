package main

import (
	"log"
	"math/rand"
	"runtime"
	"time"

	"spam-evm/config"
	"spam-evm/metrics"
	"spam-evm/network"
	"spam-evm/pkg"
	"spam-evm/types"
	"spam-evm/wallet"

	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	rand.NewSource(time.Now().UnixNano())

	cpuMultiplier := config.GetCPUMultiplier()
	oldMaxProcs := runtime.GOMAXPROCS(runtime.NumCPU() * cpuMultiplier)
	log.Printf("Changed GOMAXPROCS from %d to %d", oldMaxProcs, runtime.GOMAXPROCS(0))

	metricsData := types.NewPerformanceMetrics()

	log.Printf("CPU Cores: %d", runtime.NumCPU())
	log.Printf("GOMAXPROCS: %d", runtime.GOMAXPROCS(0))

	maxPoolConnections := config.GetMaxPoolConnections(runtime.NumCPU(), cpuMultiplier)
	log.Printf("Using max pool connections: %d", maxPoolConnections)

	clientPool := make([]*ethclient.Client, maxPoolConnections)
	for i := 0; i < maxPoolConnections; i++ {
		provider := config.RandomProvider()
		client, err := ethclient.Dial(provider)
		if err != nil {
			log.Fatalf("Failed to connect to the Ethereum client: %v", err)
		}
		clientPool[i] = client
	}

	var wallets []*types.Wallet
	privateKeys := pkg.ReadPrivateKeys()
	log.Printf("Creating %d wallets...", len(privateKeys))

	connectionStart := time.Now()
	for _, key := range privateKeys {
		clientIndex := rand.Intn(len(clientPool))
		client := clientPool[clientIndex]

		w, err := wallet.NewWallet(key, client, metricsData)
		if err != nil {
			log.Fatalf("Failed to create wallet: %v", err)
		}
		wallets = append(wallets, w)
	}

	metricsData.AddConnectionTime(time.Since(connectionStart))
	log.Printf("Wallet creation time: %v", metricsData.ConnectionTime)

	maxConcurrency := config.GetMaxConcurrency(runtime.NumCPU())
	log.Printf("Using max concurrency: %d", maxConcurrency)

	txPerWallet := config.GetTxPerWallet()
	log.Printf("Transactions per wallet: %d", txPerWallet)

	txs, perfMetrics, err := network.SpamNetwork(wallets, txPerWallet, maxConcurrency)
	if err != nil {
		log.Fatalf("Error spamming network: %v", err)
	}

	log.Printf("%d transactions sent", len(txs))
	log.Println("Spam network completed successfully")

	metrics.LogPerformanceAnalysis(perfMetrics, connectionStart)
}
