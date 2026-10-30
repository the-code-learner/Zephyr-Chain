package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/libp2p/go-libp2p/core/peer"

	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/provider"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type startupInfo struct {
	PeerID       string   `json:"peerId"`
	Addresses    []string `json:"addresses"`
	Capabilities []string `json:"capabilities"`
	Storage      string   `json:"storage"`
}

func main() {
	networkID, err := parseNetworkID(os.Getenv("ZEPHYR_NETWORK_ID"))
	if err != nil {
		log.Fatal("ZEPHYR_NETWORK_ID must be a 32-byte hex v2 NetworkID")
	}
	storage := strings.TrimSpace(os.Getenv("ZEPHYR_PROVIDER_STORAGE"))
	if storage == "" {
		storage = ".zephyr/compute-provider"
	}
	listen := strings.TrimSpace(os.Getenv("ZEPHYR_PROVIDER_LISTEN"))
	if listen == "" {
		listen = "/ip4/0.0.0.0/udp/9901/quic-v1"
	}
	allowed, err := parseProviderAdmission(os.Getenv("ZEPHYR_PROVIDER_EXPERIMENTAL_EXECUTION"), os.Getenv("ZEPHYR_PROVIDER_ALLOWED_PEERS"))
	if err != nil {
		log.Fatalf("compute provider disabled: %v", err)
	}
	identity, err := p2p.LoadOrCreateIdentity(storage, networkID)
	if err != nil {
		log.Fatalf("cannot load secure persistent transport identity: %v", err)
	}
	service, err := provider.New(provider.DiskStore{Dir: storage}, provider.HashExecutor{}, provider.IdentityExecutor{})
	if err != nil {
		log.Fatal(err)
	}
	node, err := p2p.New(p2p.Config{Network: networkID, Identity: identity, ListenAddrs: []string{listen}})
	if err != nil {
		log.Fatal(err)
	}
	defer node.Close()
	node.SetComputeHandler(func(ctx context.Context, remote peer.ID, payload []byte) ([]byte, error) {
		if err := admitComputePeer(allowed, remote); err != nil {
			return nil, err
		}
		// This experimental allowlist is NOT finalized on-chain job authorization.
		return service.Handle(ctx, payload)
	})
	addresses := make([]string, 0, len(node.Addrs()))
	for _, address := range node.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", address.String(), node.ID().String()))
	}
	info := startupInfo{PeerID: node.ID().String(), Addresses: addresses, Capabilities: []string{"sha256", "identity"}, Storage: storage}
	log.Printf("experimental compute provider: %d explicitly permitted peers; finalized job authorization is NOT implemented", len(allowed))
	encoded, _ := json.Marshal(info)
	fmt.Println(string(encoded))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

func parseNetworkID(value string) (types.NetworkID, error) {
	var network types.NetworkID
	raw, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != len(network) {
		return network, fmt.Errorf("invalid network ID")
	}
	copy(network[:], raw)
	if types.IsZero32([32]byte(network)) {
		return types.NetworkID{}, fmt.Errorf("invalid network ID")
	}
	return network, nil
}
