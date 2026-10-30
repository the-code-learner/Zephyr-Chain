package p2p

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestPersistentTransportIdentityStableAndNetworkBound(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	network := types.NetworkID(types.HashBytes("network", []byte("persistent-test")))
	first, err := LoadOrCreateIdentity(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	firstPeer, err := peer.IDFromPrivateKey(first)
	if err != nil {
		t.Fatal(err)
	}
	secondPeer, err := peer.IDFromPrivateKey(second)
	if err != nil || firstPeer != secondPeer {
		t.Fatalf("peer identity changed across restart: %q, %q, %v", firstPeer, secondPeer, err)
	}
	otherNetwork := types.NetworkID(types.HashBytes("network", []byte("another-network")))
	if _, err := LoadOrCreateIdentity(dir, otherNetwork); err == nil {
		t.Fatal("cross-network identity reuse accepted")
	}
	if third, err := LoadOrCreateIdentity(dir, network); err != nil || !first.Equals(third) {
		t.Fatalf("failed to recover original network identity: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, transportIdentityFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("insecure key mode: %v %v", info, err)
	}
}

func TestPersistentTransportIdentityRejectsCorruptionAndPermissions(t *testing.T) {
	network := types.NetworkID{1}
	for _, corrupt := range []string{"", "invalid", "ZPK1"} {
		t.Run("corrupt-"+corrupt, func(t *testing.T) {
			dir := privateIdentityTestDir(t)
			if err := os.WriteFile(filepath.Join(dir, transportIdentityFile), []byte(corrupt), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadOrCreateIdentity(dir, network); err == nil {
				t.Fatal("malformed secret was silently regenerated")
			}
		})
	}
	t.Run("world-readable-key", func(t *testing.T) {
		dir := privateIdentityTestDir(t)
		if _, err := LoadOrCreateIdentity(dir, network); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, transportIdentityFile), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateIdentity(dir, network); err == nil {
			t.Fatal("insecure key accepted")
		}
	})
	t.Run("world-readable-directory", func(t *testing.T) {
		dir := privateIdentityTestDir(t)
		if _, err := LoadOrCreateIdentity(dir, network); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateIdentity(dir, network); err == nil {
			t.Fatal("insecure directory accepted")
		}
	})
	t.Run("symlinked-key", func(t *testing.T) {
		dir := privateIdentityTestDir(t)
		target := filepath.Join(dir, "other")
		if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, transportIdentityFile)); err != nil {
			t.Skip(err)
		}
		if _, err := LoadOrCreateIdentity(dir, network); err == nil {
			t.Fatal("symlink key accepted")
		}
	})
	t.Run("zero-network", func(t *testing.T) {
		if _, err := LoadOrCreateIdentity(t.TempDir(), types.NetworkID{}); err == nil {
			t.Fatal("zero network accepted")
		}
	})
}

func privateIdentityTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "operator")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
