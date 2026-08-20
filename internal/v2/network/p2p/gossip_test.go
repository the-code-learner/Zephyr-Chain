package p2p

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestGossipTopicsAreNetworkAndShardScoped(t *testing.T) {
	a := types.NetworkID(types.HashBytes("network", []byte("a")))
	b := types.NetworkID(types.HashBytes("network", []byte("b")))
	a7, err := GossipTopic(a, 7, "tx")
	if err != nil {
		t.Fatal(err)
	}
	a8, _ := GossipTopic(a, 8, "tx")
	b7, _ := GossipTopic(b, 7, "tx")
	da7, _ := GossipTopic(a, 7, "da")
	if a7 == a8 || a7 == b7 || a7 == da7 {
		t.Fatal("gossip topic domains are not separated")
	}
}

func TestGossipTopicRejectsUnknownKind(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("topic-reject")))
	if _, err := GossipTopic(network, 0, "everything"); err != ErrGossipConfig {
		t.Fatalf("expected gossip config error, got %v", err)
	}
}
