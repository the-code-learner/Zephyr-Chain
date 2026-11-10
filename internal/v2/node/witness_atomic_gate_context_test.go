//go:build linux || darwin

package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This is only a deliberately unresponsive fixture. It neither holds
// signing keys nor supplies an independently deployed authenticated floor.
type journalBlockingWitnessFloor struct {
	entered chan struct{}
}

func (f *journalBlockingWitnessFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return FinalityCheckpoint{}, errors.New("legacy synchronous floor must not be called")
}

func (f *journalBlockingWitnessFloor) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	close(f.entered)
	<-ctx.Done()
	return FinalityCheckpoint{}, ctx.Err()
}

type journalCancelAfterQCVerifier struct {
	checkpoint FinalityCheckpoint
	cancel     context.CancelFunc
	cancelAt   int32
	calls      atomic.Int32
}

func (v *journalCancelAfterQCVerifier) VerifyFinalizedCheckpoint(cp FinalityCheckpoint) error {
	if cp != v.checkpoint {
		return ErrFinalityCheckpoint
	}
	if v.calls.Add(1) == v.cancelAt {
		v.cancel()
	}
	return nil
}

func newJournalForContextGateTest(t *testing.T, cp FinalityCheckpoint, verifier WitnessFinalityVerifier) *WitnessSigningIntentJournal {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenWitnessSigningIntentJournal(filepath.Join(dir, "intent"), cp.Network, cp.ValidatorRoot, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func TestWitnessAtomicGateContextCancelRemoteReservationKeepsDurableIntent(t *testing.T) {
	network := types.NetworkID(types.HashBytes("atomic-journal-context", []byte("reservation-network")))
	root := types.HashBytes("atomic-journal-context", []byte("reservation-root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 51, HeaderHash: types.Hash{9}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	j := newJournalForContextGateTest(t, cp, verifier)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stalled := &journalBlockingWitnessFloor{entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- ReserveAndReconcileWitnessIntentContext(ctx, j, cp, stalled)
	}()
	select {
	case <-stalled.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("remote floor was not queried after local intent fsync")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled remote reservation reported success: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled external floor held the journal lock")
	}
	if got, err := j.Current(); err != nil || got != cp {
		t.Fatalf("cancellation rolled back durable pre-sign reservation: %+v %v", got, err)
	}
	// A future authentic floor agreeing with the already-fsynced intent
	// can reconcile it: this must NOT require undoing or re-signing it.
	if err := ReserveAndReconcileWitnessIntent(j, cp, witnessFloorFixture{checkpoint: cp}); err != nil {
		t.Fatalf("canceled request leaked mutex or prevented local recovery: %v", err)
	}
}

func TestWitnessAtomicGateContextTimeoutExternalRecoveryReleasesJournal(t *testing.T) {
	network := types.NetworkID(types.HashBytes("atomic-journal-context", []byte("recovery-network")))
	root := types.HashBytes("atomic-journal-context", []byte("recovery-root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 63, HeaderHash: types.Hash{8}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	j := newJournalForContextGateTest(t, cp, verifier)
	if err := j.ReserveFinalizedIntent(cp); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	stalled := &journalBlockingWitnessFloor{entered: make(chan struct{})}
	if err := ReconcileWitnessIntentWithExternalFloorContext(ctx, j, stalled); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unresponsive floor authorized recovery: %v", err)
	}
	select {
	case <-stalled.entered:
	default:
		t.Fatal("context-aware recovery never read the external floor")
	}
	if got, err := j.Current(); err != nil || got != cp {
		t.Fatalf("recovery timeout changed the durable checkpoint: %+v %v", got, err)
	}
	if err := ReconcileWitnessIntentWithExternalFloor(j, witnessFloorFixture{checkpoint: cp}); err != nil {
		t.Fatalf("recovery cancellation did not release journal lock: %v", err)
	}
	if err := ReconcileWitnessIntentWithExternalFloorContext(nil, j, witnessFloorFixture{checkpoint: cp}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil recovery context authorized intent: %v", err)
	}
}

func TestWitnessAtomicGateContextRejectsCancellationDuringFinalQCRecheck(t *testing.T) {
	network := types.NetworkID(types.HashBytes("atomic-journal-context", []byte("qc-network")))
	root := types.HashBytes("atomic-journal-context", []byte("qc-root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 73, HeaderHash: types.Hash{5}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := &journalCancelAfterQCVerifier{checkpoint: cp, cancel: cancel, cancelAt: 2}
	j := newJournalForContextGateTest(t, cp, verifier)
	if err := ReserveAndReconcileWitnessIntentContext(ctx, j, cp, witnessFloorFixture{checkpoint: cp}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled final QC recheck authorized pre-sign reservation: %v", err)
	}
	if got, err := j.Current(); err != nil || got != cp {
		t.Fatalf("final QC cancellation removed durable intent: %+v %v", got, err)
	}
	if err := ReserveAndReconcileWitnessIntentContext(ctx, j, cp, witnessFloorFixture{checkpoint: cp}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled context authorized another journal request: %v", err)
	}
	// Reconciliation after a previous committed intent has only one QC
	// recheck. It must also fail closed if that verifier cancels its context.
	recoveryCtx, recoveryCancel := context.WithCancel(context.Background())
	defer recoveryCancel()
	verifier.cancel = recoveryCancel
	verifier.cancelAt = 3
	if err := ReconcileWitnessIntentWithExternalFloorContext(recoveryCtx, j, witnessFloorFixture{checkpoint: cp}); !errors.Is(err, context.Canceled) {
		t.Fatalf("recovery QC cancellation authorized stale evidence: %v", err)
	}
}
