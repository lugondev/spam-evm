package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"time"

	"spam-evm/config"
	"spam-evm/metrics"
	"spam-evm/network"
	"spam-evm/pkg"
	"spam-evm/types"
	"spam-evm/wallet"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
)

var (
	txPerWallet   int
	cpuMultiplier int
	keysFile      string
	providerURLs  string
)

func validateFlags() error {
	if txPerWallet <= 0 {
		return fmt.Errorf("tx-per-wallet must be greater than 0")
	}
	if cpuMultiplier <= 0 {
		return fmt.Errorf("cpu-multiplier must be greater than 0")
	}
	if keysFile == "" {
		return fmt.Errorf("keys-file is required")
	}
	if providerURLs == "" {
		return fmt.Errorf("provider-urls is required")
	}
	return nil
}

func getRandomProvider(urls []string) string {
	return urls[rand.Intn(len(urls))]
}

func runSpam() error {
	if err := validateFlags(); err != nil {
		return err
	}

	rand.NewSource(time.Now().UnixNano())

	providers := strings.Split(providerURLs, ",")
	if len(providers) == 0 {
		return fmt.Errorf("no provider URLs found")
	}
	log.Printf("Using %d provider URLs", len(providers))

	oldMaxProcs := runtime.GOMAXPROCS(runtime.NumCPU() * cpuMultiplier)
	log.Printf("Changed GOMAXPROCS from %d to %d", oldMaxProcs, runtime.GOMAXPROCS(0))

	metricsData := types.NewPerformanceMetrics()

	log.Printf("CPU Cores: %d", runtime.NumCPU())
	log.Printf("GOMAXPROCS: %d", runtime.GOMAXPROCS(0))

	maxPoolConnections := config.GetMaxPoolConnections(runtime.NumCPU(), cpuMultiplier)
	log.Printf("Using max pool connections: %d", maxPoolConnections)

	clientPool := make([]*ethclient.Client, maxPoolConnections)
	for i := 0; i < maxPoolConnections; i++ {
		provider := getRandomProvider(providers)
		client, err := ethclient.Dial(provider)
		if err != nil {
			return fmt.Errorf("failed to connect to provider %s: %v", provider, err)
		}
		clientPool[i] = client
	}

	var wallets []*types.Wallet
	privateKeys := pkg.ReadPrivateKeysFromFile(keysFile)
	if privateKeys == nil {
		return fmt.Errorf("failed to read private keys from file: %s", keysFile)
	}
	if len(privateKeys) == 0 {
		return fmt.Errorf("no private keys found in file: %s", keysFile)
	}
	log.Printf("Creating %d wallets...", len(privateKeys))

	connectionStart := time.Now()
	for _, key := range privateKeys {
		clientIndex := rand.Intn(len(clientPool))
		client := clientPool[clientIndex]

		w, err := wallet.NewWallet(key, client, metricsData)
		if err != nil {
			return fmt.Errorf("failed to create wallet: %v", err)
		}
		wallets = append(wallets, w)
	}

	metricsData.AddConnectionTime(time.Since(connectionStart))
	log.Printf("Wallet creation time: %v", metricsData.ConnectionTime)

	maxConcurrency := config.GetMaxConcurrency(runtime.NumCPU())
	log.Printf("Using max concurrency: %d", maxConcurrency)
	log.Printf("Transactions per wallet: %d", txPerWallet)

	txs, perfMetrics, err := network.SpamNetwork(wallets, txPerWallet, maxConcurrency)
	if err != nil {
		return fmt.Errorf("error spamming network: %v", err)
	}

	log.Printf("%d transactions sent", len(txs))
	log.Println("Spam network completed successfully")

	metrics.LogPerformanceAnalysis(perfMetrics, connectionStart)
	return nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "spam-evm",
		Short: "A tool for spamming EVM-compatible networks",
		Long: `spam-evm is a command-line tool for stress testing EVM-compatible networks 
by sending multiple transactions from different wallets concurrently.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpam()
		},
	}

	flags := rootCmd.Flags()
	flags.IntVar(&txPerWallet, "tx-per-wallet", 10, "Number of transactions per wallet")
	flags.IntVar(&cpuMultiplier, "cpu-multiplier", 5, "CPU core multiplier for GOMAXPROCS")
	flags.StringVar(&keysFile, "keys-file", "private-keys.txt", "File path for private keys")
	flags.StringVar(&providerURLs, "provider-urls", "", "Comma-separated list of provider URLs")

	rootCmd.MarkFlagRequired("provider-urls")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
