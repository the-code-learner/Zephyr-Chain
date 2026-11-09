//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// A deliberately unresponsive independent publisher. This fixture has no
// signing keys, external deployment or rollback-resistant storage.
type startupBlockingWitnessFloor struct {
	entered chan struct{}
	calls   atomic.Int32
}

func (f *startupBlockingWitnessFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return FinalityCheckpoint{}, errors.New("unbounded legacy path should not be called")
}

func (f *startupBlockingWitnessFloor) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	f.calls.Add(1)
	close(f.entered)
	<-ctx.Done()
	return FinalityCheckpoint{}, ctx.Err()
}

type startupCancelingQCVerifier struct {
	cancel context.CancelFunc
}

func (v startupCancelingQCVerifier) VerifyFinalizedCheckpoint(FinalityCheckpoint) error {
	v.cancel()
	return nil
}

func TestLocalWitnessCustodyStartupSharedDeadlineRejectsStalledOriginAndReleasesLock(t *testing.T) {
	network := types.NetworkID(types.HashBytes("startup-floor-deadline", []byte("network")))
	root := types.HashBytes("startup-floor-deadline", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 31, HeaderHash: types.Hash{7}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	authority, witness := witnessAtomicTestKeys()
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	witnessKey := witness.Public().(ed25519.PublicKey)
	authorityKey := authority.Public().(ed25519.PublicKey)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "signed-anchor")
	seed, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, witnessKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	signedCalls := signer.calls
	// One origin agrees with the local tip; the other never responds. A
	// shared recovery deadline must prevent any returned signing handle.
	blocked := &startupBlockingWitnessFloor{entered: make(chan struct{})}
	multi, err := NewMultiSourceWitnessFloor(&localCustodyMutableFloor{checkpoint: cp}, blocked)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	backend, err := OpenReconciledLocalWitnessCustodyBackendWithContext(ctx, path, network, root, verifier, signer, witnessKey, authorityKey, multi)
	if backend != nil || !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled origin allowed custody startup: backend=%v err=%v", backend, err)
	}
	select {
	case <-blocked.entered:
	default:
		t.Fatal("startup never requested second independently configured origin")
	}
	if blocked.calls.Load() != 1 || signer.calls != signedCalls || multi.observed {
		t.Fatal("failed startup invoked signer or promoted signed witness high-water")
	}
	// Failure must release the exclusive file lock; no in-memory signing
	// capability may remain after the remote publisher times out.
	reopened, err := OpenLocalWitnessCustodyBackend(path, network, root, false, verifier, signer, witnessKey, authorityKey)
	if err != nil {
		t.Fatalf("startup timeout leaked exclusive custody lock: %v", err)
	}
	defer reopened.Close()
	if got, err := reopened.Current(); err != nil || got != cp {
		t.Fatalf("timeout mutated durable custody anchor: %+v %v", got, err)
	}
}

func TestLocalWitnessCustodyStartupRejectsCanceledContextAndQCVerificationCancellation(t *testing.T) {
	network := types.NetworkID(types.HashBytes("startup-floor-cancel", []byte("network")))
	root := types.HashBytes("startup-floor-cancel", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 11, HeaderHash: types.Hash{2}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	authority, witness := witnessAtomicTestKeys()
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	witnessKey := witness.Public().(ed25519.PublicKey)
	authorityKey := authority.Public().(ed25519.PublicKey)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "signed-anchor")
	seed, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, witnessKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	before := signer.calls
	remote := &localCustodyMutableFloor{checkpoint: cp}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if backend, err := OpenReconciledLocalWitnessCustodyBackendWithContext(canceled, path, network, root, verifier, signer, witnessKey, authorityKey, remote); backend != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled recovery authorized signer: backend=%v err=%v", backend, err)
	}
	if backend, err := OpenReconciledLocalWitnessCustodyBackendWithContext(nil, path, network, root, verifier, signer, witnessKey, authorityKey, remote); backend != nil || !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil recovery context authorized signer: backend=%v err=%v", backend, err)
	}
	ctx, cancelQC := context.WithCancel(context.Background())
	defer cancelQC()
	if backend, err := OpenReconciledLocalWitnessCustodyBackendWithContext(ctx, path, network, root, startupCancelingQCVerifier{cancel: cancelQC}, signer, witnessKey, authorityKey, remote); backend != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during QC verification authorized signer: backend=%v err=%v", backend, err)
	}
	if signer.calls != before {
		t.Fatal("canceled startup invoked a signing provider")
	}
	reopened, err := OpenLocalWitnessCustodyBackend(path, network, root, false, verifier, signer, witnessKey, authorityKey)
	if err != nil {
		t.Fatalf("canceled startup leaked custody writer lock: %v", err)
	}
	defer reopened.Close()
}
