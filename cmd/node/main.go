package main

import (
	"log"
	"net/http"
	"os"

	"github.com/zephyr-chain/zephyr-chain/internal/api"
)

func main() {
	addr := os.Getenv("ZEPHYR_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := api.NewServer()

	log.Printf("zephyr node API listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

