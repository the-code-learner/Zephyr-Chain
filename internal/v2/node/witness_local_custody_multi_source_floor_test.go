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

// Both remote sources independently validate a canonical 3-of-4 signed
// witness bundle. This is a test of source agreement and durable recovery,
// NOT evidence that two real operators or failure domains were deployed.
func TestLocalWitnessCustodyTwoSignedSourcesRejectDivergenceAndRestore(t *testing.T) {
	network := types.NetworkID(types.HashBytes("multi-source-custody-recovery", []byte("network")))
	root := types.HashBytes("multi-source-custody-recovery", []byte("validators"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 70, HeaderHash: types.Hash{1}}
	next := first
	next.Height++
	next.HeaderHash = types.Hash{2}
	keys, private := witnessTestKeys()
	firstBundle := signedWitnessTestBundle(t, first, keys, private, 3)
	nextBundle := signedWitnessTestBundle(t, next, keys, private, 3)
	sourceA := &mutableSignedCustodyFloorSource{payload: firstBundle}
	sourceB := &mutableSignedCustodyFloorSource{payload: firstBundle}
	sourceFloor := func(source SignedWitnessFloorSource) WitnessExternalFloorVerifier {
		t.Helper()
		f, err := NewQuorumAuthenticatedWitnessFloor(source, keys, network, root)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	floorA, floorB := sourceFloor(sourceA), sourceFloor(sourceB)
	multi, err := NewMultiSourceWitnessFloor(floorA, floorB)
	if err != nil {
		t.Fatal(err)
	}
	authority, witness := witnessAtomicTestKeys()
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
		first.Height: first.HeaderHash, next.Height: next.HeaderHash,
	}}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "local-fsynced-custody")
	base, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, keyW, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.ReserveAndSignWitnessCheckpoint(context.Background(), first, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
	oldSnapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, multi)
	if err != nil {
		t.Fatalf("two agreeing pinned signed sources denied startup: %v", err)
	}
	if att, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{2}); err != nil || att.Revision != 2 {
		t.Fatalf("valid new QC tip denied after source agreement: %+v %v", att, err)
	}
	before := signer.calls
	// Source A publishes the new checkpoint; B retains an old but valid
	// 3-of-4 signature. The custodian must not pick the higher or lower one.
	sourceA.payload = nextBundle
	if _, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("two contradictory authenticated origins authorized signer: %v", err)
	}
	sourceB.payload = nextBundle
	if att, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{4}); err != nil || att.Revision != 2 {
		t.Fatalf("equal authenticated new floors did not reauthorize safe retry: %+v %v", att, err)
	}
	// Both origins now replay the OLD signed checkpoint. The multi-source
	// floor must remember the newer tip while its process remains active.
	sourceA.payload, sourceB.payload = firstBundle, firstBundle
	before = signer.calls
	if _, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{5}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("two coordinated old signed replays bypassed in-process high-water: %v", err)
	}
	if err := guarded.Close(); err != nil {
		t.Fatal(err)
	}
	// Even a new multi-source instance detects the older restored file if
	// both independently authenticated publications retain the newer tip.
	if err := os.WriteFile(path, oldSnapshot, 0600); err != nil {
		t.Fatal(err)
	}
	sourceA.payload, sourceB.payload = nextBundle, nextBundle
	freshMulti, err := NewMultiSourceWitnessFloor(floorA, floorB)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, freshMulti); !errors.Is(err, ErrWitnessSigningIntent) || b != nil {
		t.Fatalf("old local snapshot accepted despite two new pinned signed floors: %v", err)
	}
	// A forged single publication must also deny recovery.
	bad := append([]byte(nil), nextBundle...)
	bad[len(bad)-2] ^= 1
	sourceB.payload = bad
	if b, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, freshMulti); !errors.Is(err, ErrWitnessSigningIntent) || b != nil {
		t.Fatalf("forged source accepted by custody recovery: %v", err)
	}
}
