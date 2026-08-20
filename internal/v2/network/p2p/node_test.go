package p2p

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestQUICNodesUseNetworkScopedProtocolsAndBoundedFrames(t *testing.T) {
	networkID := types.NetworkID(types.HashBytes("network", []byte("p2p-test")))
	server, err := New(Config{
		Network:     networkID,
		ListenAddrs: []string{"/ip4/127.0.0.1/udp/0/quic-v1"},
		Handlers: Handlers{Transaction: func(_ context.Context, _ peer.ID, payload []byte) ([]byte, error) {
			return append([]byte("ok:"), payload...), nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := New(Config{Network: networkID, ListenAddrs: []string{"/ip4/127.0.0.1/udp/0/quic-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx, server.AddrInfo()); err != nil {
		t.Fatal(err)
	}
	got, err := client.SendTransaction(ctx, server.ID(), []byte("tx"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("ok:tx")) {
		t.Fatalf("unexpected response %q", got)
	}
	if client.TransactionProtocol() == client.ConsensusProtocol() {
		t.Fatal("protocol roles not separated")
	}
}
