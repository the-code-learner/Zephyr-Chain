//go:build linux || darwin

package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestProtectedValidatorServicePersistsSigningIntent(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	validator := types.ValidatorIDFromPublicKey(public)
	network := types.NetworkID{1}
	native := types.TokenID{2}
	set := v2consensus.ValidatorSet{Network: network, Validators: []v2consensus.Validator{
		{ID: validator, PublicKey: public, Power: 1},
	}}
	root, err := set.Root()
	if err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(t.TempDir(), "signer")
	guard, err := v2consensus.OpenDurableSigningGuard(journalDir, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	node, err := p2p.New(p2p.Config{
		Network: network, ListenAddrs: []string{"/ip4/127.0.0.1/udp/0/quic-v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	buildService := func(g *v2consensus.DurableSigningGuard) *Service {
		t.Helper()
		runtime, err := NewRuntime(network, native, root, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 1)
		if err != nil {
			t.Fatal(err)
		}
		svc, err := NewProtectedService(runtime, set, key, node, g)
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	s := buildService(guard)
	header := sharding.GlobalHeader{Version: 2, Network: network, Height: 1, ValidatorRoot: root, DataRoot: types.Hash{3}}
	p := v2consensus.Proposal{Header: header, Round: 0}
	vote, err := s.signVoteLocked(p)
	if err != nil || vote.HeaderHash != v2consensus.HeaderConsensusHash(header) {
		t.Fatalf("protected vote signing failed: %+v, %v", vote, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	another, err := v2consensus.OpenDurableSigningGuard(journalDir, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	defer another.Close()
	next := buildService(another)
	changed := p
	changed.Header.DataRoot = types.Hash{9}
	if _, err := next.signVoteLocked(changed); !errors.Is(err, v2consensus.ErrSigningConflict) {
		t.Fatalf("protected service double-signed across restart: %v", err)
	}
	if _, err := next.signVoteLocked(p); err != nil {
		t.Fatalf("same target not retryable after restart: %v", err)
	}
	stale := p
	stale.Header.Height = 0
	if _, err := next.signVoteLocked(stale); !errors.Is(err, v2consensus.ErrSigningConflict) {
		t.Fatalf("stale signature accepted after restart: %v", err)
	}
	if _, err := NewProtectedService(next.Runtime, set, key, node, nil); err == nil {
		t.Fatal("unguarded validator constructor accepted")
	}
	if _, err := NewProtectedService(next.Runtime, set, key, node, guard); err == nil {
		t.Fatal("closed signing guard accepted")
	}
}
