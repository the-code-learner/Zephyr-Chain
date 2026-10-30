package main

import (
	"crypto/rand"
	"testing"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func testProviderPeer(t *testing.T) peer.ID {
	t.Helper()
	key, _, err := p2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProviderAdmissionFailsClosed(t *testing.T) {
	a := testProviderPeer(t)
	b := testProviderPeer(t)
	for _, item := range []struct{ enabled, peers string }{
		{"", a.String()},
		{"false", a.String()},
		{"TRUE", a.String()},
		{"true", ""},
		{"true", "bad peer id !"},
		{"true", a.String() + ","},
		{"true", a.String() + "," + a.String()},
	} {
		if _, err := parseProviderAdmission(item.enabled, item.peers); err == nil {
			t.Fatalf("insecure admission enabled=%q peers=%q was accepted", item.enabled, item.peers)
		}
	}
	allowed, err := parseProviderAdmission("true", " "+a.String()+" , "+b.String()+" ")
	if err != nil || len(allowed) != 2 {
		t.Fatalf("invalid exact allowlist: %v", err)
	}
	if err := admitComputePeer(allowed, a); err != nil {
		t.Fatalf("allowed peer rejected: %v", err)
	}
	if err := admitComputePeer(allowed, b); err != nil {
		t.Fatalf("allowed peer rejected: %v", err)
	}
	if err := admitComputePeer(allowed, testProviderPeer(t)); err == nil {
		t.Fatal("unlisted peer was permitted to execute")
	}
	if err := admitComputePeer(allowed, ""); err == nil {
		t.Fatal("empty peer identity was permitted to execute")
	}
}
