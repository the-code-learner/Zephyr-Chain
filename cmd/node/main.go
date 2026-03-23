package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/api"
)

func main() {
	addr := os.Getenv("ZEPHYR_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	config := api.DefaultConfig()
	if dataDir := os.Getenv("ZEPHYR_DATA_DIR"); dataDir != "" {
		config.DataDir = dataDir
	}
	if interval := os.Getenv("ZEPHYR_BLOCK_INTERVAL"); interval != "" {
		parsed, err := time.ParseDuration(interval)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_BLOCK_INTERVAL %q: %v", interval, err)
		}
		config.BlockInterval = parsed
	}
	if maxTxs := os.Getenv("ZEPHYR_MAX_TXS_PER_BLOCK"); maxTxs != "" {
		parsed, err := strconv.Atoi(maxTxs)
		if err != nil || parsed <= 0 {
			log.Fatalf("invalid ZEPHYR_MAX_TXS_PER_BLOCK %q", maxTxs)
		}
		config.MaxTransactionsPerBlock = parsed
	}

	server, err := api.NewServerWithConfig(config)
	if err != nil {
		log.Fatalf("unable to create node server: %v", err)
	}
	defer server.Close()

	log.Printf("zephyr node API listening on %s (data dir: %s, block interval: %s)", addr, config.DataDir, config.BlockInterval)
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
