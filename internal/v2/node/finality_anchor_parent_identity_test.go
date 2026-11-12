//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// A different private directory can be placed under the same absolute path,
// then the *original* locked file moved back into it. The pathname, file inode,
// hardlink count and complete on-disk hash chain remain valid, but the original
// directory custody boundary has been replaced. No private signing is allowed.
func TestLocalWitnessCustodyRejectsReplacedParentDirectoryBeforeSigning(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("anchor-replaced-parent", []byte("network")))
	root := types.HashBytes("anchor-replaced-parent", []byte("root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 7, HeaderHash: types.Hash{1}}
	later := first
	later.Height = 8
	later.HeaderHash = types.Hash{2}
	base := t.TempDir()
	parent := filepath.Join(base, "custody")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "anchor")
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
		first.Height: first.HeaderHash, later.Height: later.HeaderHash,
	}}
	backend, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer,
		witness.Public().(ed25519.PublicKey), authority.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), first, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	before := signer.calls
	lockedFile, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	movedParent := filepath.Join(base, "custody-displaced")
	if err := os.Rename(parent, movedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(movedParent, "anchor"), path); err != nil {
		t.Fatal(err)
	}
	// This is a stronger attack than substituting another anchor inode:
	// every check that looked only at the anchor file still passes.
	restoredFile, err := os.Lstat(path)
	if err != nil || !os.SameFile(lockedFile, restoredFile) {
		t.Fatalf("attack fixture must retain original anchor inode: %v", err)
	}
	for _, cp := range []FinalityCheckpoint{first, later} {
		if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{2}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
			t.Fatalf("replaced parent authorized private signing height=%d: err=%v extra calls=%d", cp.Height, err, signer.calls-before)
		}
	}
	if _, err := backend.Current(); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("replaced parent exposed trusted custody floor: %v", err)
	}
	// Reconstruct the exact original directory and inode: poisoning persists.
	if err := os.Rename(path, filepath.Join(movedParent, "anchor")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(movedParent, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), later, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("restored parent unpoisoned signer: err=%v extra calls=%d", err, signer.calls-before)
	}
}

func TestFinalityAnchorStoreRejectsParentModeDrift(t *testing.T) {
	network := types.NetworkID(types.HashBytes("anchor-parent-mode", []byte("network")))
	root := types.Hash{2}
	path := privateAnchorTestPath(t)
	anchor, err := OpenFinalityAnchorStore(path, network, root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.Close()
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 1, HeaderHash: types.Hash{1}}
	if err := anchor.advance(first); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(path)
	if err := os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := anchor.Current(); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("new public directory mode accepted: %v", err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := anchor.advance(first); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("mode restoration unpoisoned anchor: %v", err)
	}
}
