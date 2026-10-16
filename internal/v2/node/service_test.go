package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestConsensusServiceFinalizesAcrossThreeQUICValidators(t *testing.T) {
	networkID := types.NetworkID(types.HashBytes("network", []byte("service-three")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	keys := make([]*ecdsa.PrivateKey, 3)
	validators := v2consensus.ValidatorSet{Network: networkID, Validators: make([]v2consensus.Validator, 3)}
	for i := range keys {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = key
		public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		validators.Validators[i] = v2consensus.Validator{ID: types.ValidatorIDFromPublicKey(public), PublicKey: public, Power: 10}
	}
	validatorRoot, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}

	nodes := make([]*p2p.Node, 3)
	services := make([]*Service, 3)
	for i := range nodes {
		node, err := p2p.New(p2p.Config{Network: networkID, ListenAddrs: []string{"/ip4/127.0.0.1/udp/0/quic-v1"}, MaxMessageBytes: MaxNetworkMessageBytes})
		if err != nil {
			t.Fatal(err)
		}
		nodes[i] = node
		defer node.Close()
		runtime, err := NewRuntime(networkID, native, validatorRoot, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 2)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(runtime, validators, keys[i], node)
		if err != nil {
			t.Fatal(err)
		}
		services[i] = service
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := range nodes {
		for j := range nodes {
			if i == j {
				continue
			}
			if err := nodes[i].Connect(ctx, nodes[j].AddrInfo()); err != nil {
				t.Fatal(err)
			}
		}
		peerIDs := make([]peer.ID, 0, len(nodes)-1)
		for j := range nodes {
			if i != j {
				peerIDs = append(peerIDs, nodes[j].ID())
			}
		}
		services[i].SetPeers(peerIDs)
	}
	expected, err := validators.Proposer(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	proposer := -1
	for i, key := range keys {
		id, _ := validatorID(key)
		if id == expected.ID {
			proposer = i
			break
		}
	}
	if proposer < 0 {
		t.Fatal("scheduled proposer not found")
	}
	snapshot, err := services[proposer].Propose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Header.Height != 1 {
		t.Fatalf("unexpected finalized height %d", snapshot.Header.Height)
	}
	for i, service := range services {
		if service.Runtime.Height != 1 {
			t.Fatalf("validator %d did not commit height 1", i)
		}
		latest, err := service.LatestSnapshot()
		if err != nil || latest.Header.Height != 1 {
			t.Fatalf("validator %d has no finalized snapshot: %v", i, err)
		}
	}
}
