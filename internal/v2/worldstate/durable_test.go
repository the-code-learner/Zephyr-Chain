package worldstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDiskPersistsWALAndCheckpoint(t *testing.T) {
	dir := t.TempDir()
	network := types.NetworkID(types.HashBytes("network", []byte("disk-test")))
	owner := types.AccountIDFromPublicKey([]byte("owner"))
	token := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	id := types.ObjectIDFromTransaction(types.HashBytes("seed", []byte("coin")), 0)
	out, err := object.NewCoinOutput(owner, token, 100)
	if err != nil {
		t.Fatal(err)
	}
	coin := object.Object{ID: id, Version: 1, Owner: owner, Kind: out.Kind, Data: out.Data}

	store, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Apply(nil, []object.Object{coin})
	if err != nil {
		t.Fatal(err)
	}
	if store.Sequence() != 1 {
		t.Fatalf("expected sequence 1")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Root() != root {
		t.Fatal("WAL replay changed root")
	}
	if _, ok := reopened.GetObject(id); !ok {
		t.Fatal("WAL replay lost object")
	}
	if err := reopened.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	afterCheckpoint, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if afterCheckpoint.Root() != root || afterCheckpoint.Sequence() != 1 {
		t.Fatal("checkpoint restore changed state")
	}
	if err := afterCheckpoint.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiskRejectsWrongNetworkAndIgnoresTornTail(t *testing.T) {
	dir := t.TempDir()
	network := types.NetworkID(types.HashBytes("network", []byte("a")))
	store, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	wrong := types.NetworkID(types.HashBytes("network", []byte("b")))
	if _, err := OpenDisk(dir, wrong); !errors.Is(err, ErrPersistenceNetwork) {
		t.Fatalf("expected wrong-network rejection, got %v", err)
	}

	walPath := filepath.Join(dir, "state.wal")
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("ZWL2\x00")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenDisk(dir, network)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Sequence() != 0 {
		t.Fatal("torn tail advanced sequence")
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("torn WAL tail was not truncated: %d", info.Size())
	}
}
