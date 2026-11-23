package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sovrack/agent/internal/config"
	"github.com/sovrack/agent/internal/executor"
	"github.com/sovrack/agent/internal/metrics"
	"github.com/sovrack/agent/internal/tunnel"
)

const Version = "3.1.0"

func main() {
	fmt.Printf("\n=== Sovrack Outbound Tunnel Agent v%s ===\n", Version)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[FATAL] Configuration error: %v", err)
	}

	collector := metrics.NewCollector()
	hostInfo := collector.GetHostInfo()
	log.Printf("[Init] Host detected: %s (%s %s, %d CPU cores)",
		hostInfo.Hostname, hostInfo.OS, hostInfo.Arch, hostInfo.CPUCores)
	log.Printf("[Init] Target Sovrack gateway: %s", cfg.ServerURL)

	runner := executor.NewRunner()
	client := tunnel.NewClient(cfg, collector, runner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("[Shutdown] Received signal: %v. Shutting down Sovrack Agent gracefully...", sig)
		cancel()
		client.Close()
	}()

	// Start tunnel
	client.Start(ctx)
	log.Println("[Shutdown] Sovrack Agent stopped cleanly.")
}
