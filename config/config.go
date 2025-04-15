package config

func GetMaxPoolConnections(cpuCores int, cpuMultiplier int) int {
	return cpuCores * cpuMultiplier
}

func GetMaxConcurrency(cpuCores int) int {
	return cpuCores * 500
}
