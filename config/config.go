package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TxPerWallet   int      `yaml:"txPerWallet"`
	CpuMultiplier int      `yaml:"cpuMultiplier"`
	KeysFile      string   `yaml:"keysFile"`
	ProviderURLs  []string `yaml:"providerUrls"`
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		return &Config{
			TxPerWallet:   10,
			CpuMultiplier: 5,
			KeysFile:      "private-keys.txt",
		}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if c.TxPerWallet <= 0 {
		return fmt.Errorf("tx-per-wallet must be greater than 0")
	}
	if c.CpuMultiplier <= 0 {
		return fmt.Errorf("cpu-multiplier must be greater than 0")
	}
	if c.KeysFile == "" {
		return fmt.Errorf("keys-file is required")
	}
	if len(c.ProviderURLs) == 0 {
		return fmt.Errorf("provider-urls is required")
	}
	return nil
}

func GetMaxPoolConnections(cpuCores int, cpuMultiplier int) int {
	return cpuCores * cpuMultiplier
}

func GetMaxConcurrency(cpuCores int) int {
	return cpuCores * 500
}
