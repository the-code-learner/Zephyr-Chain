package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/api"
)

func main() {
	addr := os.Getenv("ZEPHYR_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	config := api.DefaultConfig()
	if nodeID := os.Getenv("ZEPHYR_NODE_ID"); nodeID != "" {
		config.NodeID = nodeID
	}
	if validatorAddress := os.Getenv("ZEPHYR_VALIDATOR_ADDRESS"); validatorAddress != "" {
		config.ValidatorAddress = validatorAddress
	}
	if validatorPrivateKey := os.Getenv("ZEPHYR_VALIDATOR_PRIVATE_KEY"); validatorPrivateKey != "" {
		config.ValidatorPrivateKey = validatorPrivateKey
	}
	if dataDir := os.Getenv("ZEPHYR_DATA_DIR"); dataDir != "" {
		config.DataDir = dataDir
	}
	if peers := os.Getenv("ZEPHYR_PEERS"); peers != "" {
		config.PeerURLs = splitCSV(peers)
	}
	if bindings := os.Getenv("ZEPHYR_PEER_VALIDATORS"); bindings != "" {
		parsed, err := splitPeerValidatorBindings(bindings)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_PEER_VALIDATORS %q: %v", bindings, err)
		}
		config.PeerValidatorBindings = parsed
	}
	if interval := os.Getenv("ZEPHYR_BLOCK_INTERVAL"); interval != "" {
		parsed, err := time.ParseDuration(interval)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_BLOCK_INTERVAL %q: %v", interval, err)
		}
		config.BlockInterval = parsed
	}
	if syncInterval := os.Getenv("ZEPHYR_SYNC_INTERVAL"); syncInterval != "" {
		parsed, err := time.ParseDuration(syncInterval)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_SYNC_INTERVAL %q: %v", syncInterval, err)
		}
		config.SyncInterval = parsed
	}
	if maxTxs := os.Getenv("ZEPHYR_MAX_TXS_PER_BLOCK"); maxTxs != "" {
		parsed, err := strconv.Atoi(maxTxs)
		if err != nil || parsed <= 0 {
			log.Fatalf("invalid ZEPHYR_MAX_TXS_PER_BLOCK %q", maxTxs)
		}
		config.MaxTransactionsPerBlock = parsed
	}
	if enabled := os.Getenv("ZEPHYR_ENABLE_BLOCK_PRODUCTION"); enabled != "" {
		parsed, err := strconv.ParseBool(enabled)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_ENABLE_BLOCK_PRODUCTION %q", enabled)
		}
		config.EnableBlockProduction = parsed
	}
	if enabled := os.Getenv("ZEPHYR_ENABLE_PEER_SYNC"); enabled != "" {
		parsed, err := strconv.ParseBool(enabled)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_ENABLE_PEER_SYNC %q", enabled)
		}
		config.EnablePeerSync = parsed
	}
	if enabled := os.Getenv("ZEPHYR_REQUIRE_PEER_IDENTITY"); enabled != "" {
		parsed, err := strconv.ParseBool(enabled)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_REQUIRE_PEER_IDENTITY %q", enabled)
		}
		config.RequirePeerIdentity = parsed
	}
	if enabled := os.Getenv("ZEPHYR_ENFORCE_PROPOSER_SCHEDULE"); enabled != "" {
		parsed, err := strconv.ParseBool(enabled)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_ENFORCE_PROPOSER_SCHEDULE %q", enabled)
		}
		config.EnforceProposerSchedule = parsed
	}
	if enabled := os.Getenv("ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES"); enabled != "" {
		parsed, err := strconv.ParseBool(enabled)
		if err != nil {
			log.Fatalf("invalid ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES %q", enabled)
		}
		config.RequireConsensusCertificates = parsed
	}

	server, err := api.NewServerWithConfig(config)
	if err != nil {
		log.Fatalf("unable to create node server: %v", err)
	}
	defer server.Close()

	log.Printf(
		"zephyr node %s listening on %s (validator: %s, data dir: %s, block interval: %s, peer sync: %t, peer identity required: %t, peer bindings: %d, proposer schedule enforced: %t, consensus certificates required: %t, peers: %d)",
		config.NodeID,
		addr,
		config.ValidatorAddress,
		config.DataDir,
		config.BlockInterval,
		config.EnablePeerSync,
		config.RequirePeerIdentity || len(config.PeerValidatorBindings) > 0,
		len(config.PeerValidatorBindings),
		config.EnforceProposerSchedule,
		config.RequireConsensusCertificates,
		len(config.PeerURLs),
	)
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered
}

func splitPeerValidatorBindings(value string) (map[string]string, error) {
	parts := strings.Split(value, ",")
	bindings := make(map[string]string, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.SplitN(part, "=", 2)
		if len(segments) != 2 {
			return nil, fmt.Errorf("expected <peer-url>=<validator-address>")
		}
		peerURL := strings.TrimSpace(segments[0])
		validator := strings.TrimSpace(segments[1])
		if peerURL == "" || validator == "" {
			return nil, fmt.Errorf("expected <peer-url>=<validator-address>")
		}
		bindings[peerURL] = validator
	}
	return bindings, nil
}
