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
	cfg           *config.Config
	configFile    string
	txPerWallet   int
	cpuMultiplier int
	keysFile      string
	providerURLs  string
)

func loadConfiguration() error {
	var err error
	cfg, err = config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// Override config with CLI flags if provided
	if txPerWallet > 0 {
		cfg.TxPerWallet = txPerWallet
	}
	if cpuMultiplier > 0 {
		cfg.CpuMultiplier = cpuMultiplier
	}
	if keysFile != "" {
		cfg.KeysFile = keysFile
	}
	if providerURLs != "" {
		cfg.ProviderURLs = strings.Split(providerURLs, ",")
	}

	return cfg.Validate()
}

func getRandomProvider(urls []string) string {
	return urls[rand.Intn(len(urls))]
}

func runSpam() error {
	if err := loadConfiguration(); err != nil {
		return err
	}

	rand.NewSource(time.Now().UnixNano())

	if len(cfg.ProviderURLs) == 0 {
		return fmt.Errorf("no provider URLs found")
	}
	log.Printf("Using %d provider URLs", len(cfg.ProviderURLs))

	oldMaxProcs := runtime.GOMAXPROCS(runtime.NumCPU() * cfg.CpuMultiplier)
	log.Printf("Changed GOMAXPROCS from %d to %d", oldMaxProcs, runtime.GOMAXPROCS(0))

	metricsData := types.NewPerformanceMetrics()

	log.Printf("CPU Cores: %d", runtime.NumCPU())
	log.Printf("GOMAXPROCS: %d", runtime.GOMAXPROCS(0))

	maxPoolConnections := config.GetMaxPoolConnections(runtime.NumCPU(), cfg.CpuMultiplier)
	log.Printf("Using max pool connections: %d", maxPoolConnections)

	clientPool := make([]*ethclient.Client, maxPoolConnections)
	for i := 0; i < maxPoolConnections; i++ {
		provider := getRandomProvider(cfg.ProviderURLs)
		client, err := ethclient.Dial(provider)
		if err != nil {
			return fmt.Errorf("failed to connect to provider %s: %v", provider, err)
		}
		clientPool[i] = client
	}

	var wallets []*types.Wallet
	privateKeys := pkg.ReadPrivateKeysFromFile(cfg.KeysFile)
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
	log.Printf("Transactions per wallet: %d", cfg.TxPerWallet)

	txs, perfMetrics, err := network.SpamNetwork(wallets, cfg.TxPerWallet, maxConcurrency)
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
	flags.StringVar(&configFile, "config", "config.yaml", "Path to YAML config file")
	flags.IntVar(&txPerWallet, "tx-per-wallet", 0, "Number of transactions per wallet")
	flags.IntVar(&cpuMultiplier, "cpu-multiplier", 0, "CPU core multiplier for GOMAXPROCS")
	flags.StringVar(&keysFile, "keys-file", "", "File path for private keys")
	flags.StringVar(&providerURLs, "provider-urls", "", "Comma-separated list of provider URLs")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
