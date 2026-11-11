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

// These checks protect only the local reference anchor's inode. They cannot
// establish remote/HSM monotonic signing or resistance to whole-file restore.
func TestFinalityAnchorRejectsExistingHardLinkBeforeRecovery(t *testing.T) {
	network := types.NetworkID(types.HashBytes("anchor-hardlink-open", []byte("network")))
	root := types.Hash{7}
	path := privateAnchorTestPath(t)
	anchor, err := OpenFinalityAnchorStore(path, network, root, true)
	if err != nil {
		t.Fatal(err)
	}
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 1, HeaderHash: types.Hash{1}}
	if err := anchor.advance(cp); err != nil {
		t.Fatal(err)
	}
	if err := anchor.Close(); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(path), "hardlink-backup")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenFinalityAnchorStore(path, network, root, false); reopened != nil || !errors.Is(err, ErrFinalityAnchor) {
		if reopened != nil {
			_ = reopened.Close()
		}
		t.Fatalf("multiply linked custody anchor reopened: backend=%v err=%v", reopened, err)
	}
	if reopened, err := OpenFinalityAnchorStore(alias, network, root, false); reopened != nil || !errors.Is(err, ErrFinalityAnchor) {
		if reopened != nil {
			_ = reopened.Close()
		}
		t.Fatalf("hard-link alias acquired custody signing access: backend=%v err=%v", reopened, err)
	}
}

func TestLocalWitnessCustodyAnchorHardLinkPoisonsLiveSignerBeforeKeyUse(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("anchor-hardlink-live", []byte("network")))
	root := types.HashBytes("anchor-hardlink-live", []byte("validator-root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 8, HeaderHash: types.Hash{1}}
	next := first
	next.Height++
	next.HeaderHash = types.Hash{2}
	path := privateAnchorTestPath(t)
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{first.Height: first.HeaderHash, next.Height: next.HeaderHash}}
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
	alias := filepath.Join(filepath.Dir(path), "unexpected-hardlink")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	// Both an idempotent retry and a new finalized height must fail without
	// another private-key invocation. Removing the alias cannot unpoison the
	// already exposed signing handle.
	for _, cp := range []FinalityCheckpoint{first, next} {
		if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{2}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
			t.Fatalf("multiply linked anchor permitted private-key signing: height=%d err=%v calls=%d", cp.Height, err, signer.calls-before)
		}
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("removal of hard link unpoisoned signer: err=%v calls=%d", err, signer.calls-before)
	}
	if _, err := backend.Current(); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("poisoned custody anchor exposed trusted current state: %v", err)
	}
}
