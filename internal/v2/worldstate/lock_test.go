package worldstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDurableStoreRejectsConcurrentWritersAndReleasesOnClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	network := types.NetworkID{1}
	first, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDisk(dir, network); !errors.Is(err, ErrPersistenceBusy) {
		t.Fatalf("second writer accepted or wrong error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal("idempotent close failed:", err)
	}
	reopened, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatalf("writer lock was not released: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDurableLockRejectsSymlinkAndExposedPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	network := types.NetworkID{1}
	target := filepath.Join(dir, "external")
	if err := os.WriteFile(target, []byte("not a real lock"), 0600); err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(dir, "state.writer.lock")
	if err := os.Symlink(target, lockFile); err != nil {
		t.Skip(err)
	}
	if _, err := OpenDisk(dir, network); err == nil {
		t.Fatal("symlink writer lock accepted")
	}
	if err := os.Remove(lockFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockFile, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDisk(dir, network); !errors.Is(err, ErrPersistenceBusy) {
		t.Fatalf("world-readable lock accepted: %v", err)
	}
}

func TestDurableWriterLockNotPoisonedByForeignNetworkOpen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	network := types.NetworkID{1}
	d, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	// Initial state WAL is empty and not network-bound until first apply;
	// a checkpoint commits the network for the foreign-open regression.
	d, err = OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDisk(dir, types.NetworkID{2}); !errors.Is(err, ErrPersistenceNetwork) {
		t.Fatalf("wrong-network checkpoint accepted: %v", err)
	}
	good, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatalf("failed to reopen after rejected foreign checkpoint: %v", err)
	}
	if err := good.Close(); err != nil {
		t.Fatal(err)
	}
}
