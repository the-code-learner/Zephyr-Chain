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

// This fixture is a CONTRACT test only. Production must use an external
// quorum-authenticated source with independently provisioned signing keys.
type localCustodyMutableFloor struct {
	checkpoint FinalityCheckpoint
	err        error
}

func (f *localCustodyMutableFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.checkpoint, f.err
}

func TestLocalWitnessCustodyExternalFloorDetectsRestoredOldSnapshot(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-external-recovery", []byte("network")))
	root := types.HashBytes("custody-external-recovery", []byte("root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 21, HeaderHash: types.Hash{1}}
	later := first
	later.Height = 22
	later.HeaderHash = types.Hash{2}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
		first.Height: first.HeaderHash, later.Height: later.HeaderHash,
	}}
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "locked-anchor")
	keyW := witness.Public().(ed25519.PublicKey)
	keyA := authority.Public().(ed25519.PublicKey)
	unfenced, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, keyW, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unfenced.ReserveAndSignWitnessCheckpoint(context.Background(), first, [32]byte{1}); err != nil {
		t.Fatalf("seed first durable checkpoint: %v", err)
	}
	if err := unfenced.Close(); err != nil {
		t.Fatal(err)
	}
	oldSnapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	remote := &localCustodyMutableFloor{checkpoint: first}
	guarded, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, remote)
	if err != nil {
		t.Fatalf("exact authenticated external floor was rejected: %v", err)
	}
	// This is the STRICT publication gate: the first new signing is allowed
	// with an exact floor, but further signatures must wait for publication
	// of the new durable tip by the independent floor authority.
	att, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), later, [32]byte{2})
	if err != nil || att.Revision != 2 {
		t.Fatalf("new checkpoint rejected after exact reconciliation: %+v %v", att, err)
	}
	before := signer.calls
	if _, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), later, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("stale external floor allowed a second signer call: %v", err)
	}
	remote.checkpoint = later
	if att, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), later, [32]byte{4}); err != nil || att.Revision != 2 {
		t.Fatalf("independent publication did not reauthorize idempotent retry: %+v %v", att, err)
	}
	if err := guarded.Close(); err != nil {
		t.Fatal(err)
	}
	// Replacing the ENTIRE historical ledger with an older, valid snapshot
	// cannot be detected by the file itself. The independent external floor
	// must catch it BEFORE the restored backend can touch signing keys.
	if err := os.WriteFile(path, oldSnapshot, 0600); err != nil {
		t.Fatal(err)
	}
	before = signer.calls
	if b, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, remote); !errors.Is(err, ErrWitnessSigningIntent) || b != nil {
		t.Fatalf("old fully valid snapshot bypassed independent recovery: backend=%v err=%v", b, err)
	}
	if signer.calls != before {
		t.Fatal("rollback recovery invoked signer")
	}
	// A failed reconciliation must release the exclusive file lock so
	// later operator remediation can take place without lock leakage.
	b, err := OpenLocalWitnessCustodyBackend(path, network, root, false, verifier, signer, keyW, keyA)
	if err != nil {
		t.Fatalf("failed recovery leaked file lock: %v", err)
	}
	defer b.Close()
	if current, err := b.Current(); err != nil || current != first {
		t.Fatalf("fixture did not actually restore old checkpoint: %+v %v", current, err)
	}
}

func TestLocalWitnessCustodyExternalFloorFailsClosedOnMissingAndContradictorySources(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-external-refusal", []byte("network")))
	root := types.HashBytes("custody-external-refusal", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 42, HeaderHash: types.Hash{3}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "locked-anchor")
	keyW := witness.Public().(ed25519.PublicKey)
	keyA := authority.Public().(ed25519.PublicKey)
	open := func(remote WitnessExternalFloorVerifier) (*LocalWitnessCustodyBackend, error) {
		return OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, remote)
	}
	if b, err := open(nil); !errors.Is(err, ErrWitnessSigningIntent) || b != nil {
		t.Fatalf("missing independent source accepted: %v", err)
	}
	initial, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, keyW, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := open(&localCustodyMutableFloor{checkpoint: cp}); err == nil || b != nil {
		t.Fatal("empty / exclusively locked anchor accepted by recovery gate")
	}
	if _, err := initial.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}
	stale := cp
	stale.Height--
	future := cp
	future.Height++
	conflict := cp
	conflict.HeaderHash[0]++
	foreign := cp
	foreign.Network[0]++
	for _, test := range []struct {
		name   string
		remote *localCustodyMutableFloor
	}{
		{"unavailable", &localCustodyMutableFloor{err: errors.New("offline")}},
		{"stale", &localCustodyMutableFloor{checkpoint: stale}},
		{"ahead", &localCustodyMutableFloor{checkpoint: future}},
		{"conflicting", &localCustodyMutableFloor{checkpoint: conflict}},
		{"foreign", &localCustodyMutableFloor{checkpoint: foreign}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := signer.calls
			if b, err := open(test.remote); !errors.Is(err, ErrWitnessSigningIntent) || b != nil {
				t.Fatalf("unsafe external floor authorized signing: %v", err)
			}
			if signer.calls != before {
				t.Fatal("rejected external floor invoked signer")
			}
		})
	}
	remote := &localCustodyMutableFloor{checkpoint: cp}
	b, err := open(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	remote.err = errors.New("network loss")
	before := signer.calls
	if _, err := b.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{2}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("live external source failure did not fence signer: %v", err)
	}
}
