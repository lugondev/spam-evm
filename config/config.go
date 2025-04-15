package config

import (
	"math/rand"
)

var Providers = []string{
	"http://34.87.31.105:4001",
	"http://34.87.31.105:4002",
}

func RandomProvider() string {
	randomIndex := rand.Intn(len(Providers))
	return Providers[randomIndex]
}

func GetCPUMultiplier() int {
	return 5
}

func GetMaxPoolConnections(cpuCores int, cpuMultiplier int) int {
	return cpuCores * cpuMultiplier
}

func GetMaxConcurrency(cpuCores int) int {
	return cpuCores * 500
}
