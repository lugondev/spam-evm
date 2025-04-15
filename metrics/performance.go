package metrics

import (
	"log"
	"runtime"
	"time"

	"spam-evm/types"
)

func MonitorResources(stopCh <-chan struct{}, frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	var m runtime.MemStats
	for {
		select {
		case <-ticker.C:
			runtime.ReadMemStats(&m)
			log.Printf("Memory: Alloc = %v MiB, TotalAlloc = %v MiB, Sys = %v MiB, NumGC = %v",
				m.Alloc/1024/1024,
				m.TotalAlloc/1024/1024,
				m.Sys/1024/1024,
				m.NumGC)
			log.Printf("Goroutines: %d", runtime.NumGoroutine())
		case <-stopCh:
			return
		}
	}
}

func LogPerformanceAnalysis(metrics *types.PerformanceMetrics, startTime time.Time) {
	log.Println("\n--- PERFORMANCE ANALYSIS ---")
	totalTime := time.Since(startTime)
	log.Printf("Connection setup: %.2fms", float64(metrics.ConnectionTime.Milliseconds()))
	log.Printf("Transaction signing: %.2fms (%.2f%%)",
		float64(metrics.SignTime.Milliseconds()),
		float64(metrics.SignTime.Milliseconds())/float64(totalTime.Milliseconds())*100)
	log.Printf("Transaction sending: %.2fms (%.2f%%)",
		float64(metrics.SendTime),
		float64(metrics.SendTime)/float64(totalTime.Milliseconds())*100)
}
