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

// A moved anchor retains ONE link and a valid fstat, so link-count checks
// alone cannot prevent writing a journal no longer at the operator-pinned
// canonical path. These are local reference tests, NOT an HSM rollback proof.
func TestFinalityAnchorStoreRejectsRenamedCanonicalPath(t *testing.T) {
	network := types.NetworkID(types.HashBytes("renamed-anchor-path", []byte("network")))
	root := types.Hash{8}
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
	moved := filepath.Join(filepath.Dir(path), "displaced-anchor")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := anchor.Current(); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("renamed journal accepted via open inode: %v", err)
	}
	second := first
	second.Height++
	second.HeaderHash = types.Hash{2}
	if err := anchor.advance(second); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("renamed journal allowed a new finalized reservation: %v", err)
	}
	if err := os.Rename(moved, path); err != nil {
		t.Fatal(err)
	}
	if _, err := anchor.Current(); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("restoring journal pathname unpoisoned active handle: %v", err)
	}
}

func TestLocalWitnessCustodyRejectsReplacedCanonicalPathBeforeSigning(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("replaced-custody-path", []byte("network")))
	root := types.HashBytes("replaced-custody-path", []byte("root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 7, HeaderHash: types.Hash{1}}
	later := first
	later.Height++
	later.HeaderHash = types.Hash{2}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "canonical-custody-anchor")
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{first.Height: first.HeaderHash, later.Height: later.HeaderHash}}
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
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(dir, "moved-but-still-single-link")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	// The substitute contains an identically valid checkpoint and mode,
	// but is a different inode. The old handle must NOT sign into moved.
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, cp := range []FinalityCheckpoint{first, later} {
		if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{2}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
			t.Fatalf("replaced canonical custody path authorized signer: height=%d err=%v calls=%d", cp.Height, err, signer.calls-before)
		}
	}
	if _, err := backend.Current(); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("replaced canonical path exposed trusted current checkpoint: %v", err)
	}
}
