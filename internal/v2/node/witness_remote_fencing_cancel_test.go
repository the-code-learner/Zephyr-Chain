//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// The verifier can cancel a caller during its final QC recheck; a signed
// remote grant must not turn a canceled operation into an apparent success.
type witnessCancelOnRecheckVerifier struct {
	checkpoint FinalityCheckpoint
	cancel     context.CancelFunc
	calls      int
	cancelAt   int
}

func (v *witnessCancelOnRecheckVerifier) VerifyFinalizedCheckpoint(cp FinalityCheckpoint) error {
	if cp != v.checkpoint {
		return ErrFinalityCheckpoint
	}
	v.calls++
	if v.calls == v.cancelAt {
		v.cancel()
	}
	return nil
}

func TestWitnessFenceRejectsCancellationDuringFinalQCRecheck(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-cancel-recheck", []byte("network")))
	root := types.HashBytes("witness-cancel-recheck", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 31, HeaderHash: types.Hash{7}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := &witnessCancelOnRecheckVerifier{checkpoint: cp, cancel: cancel, cancelAt: 2}
	journal := witnessFenceTestJournal(t, cp, verifier)
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 29
	private := ed25519.NewKeyFromSeed(seed)
	remote := &witnessFenceAuthorityFixture{private: private}
	err := ReserveAndVerifyWitnessFence(ctx, journal, cp, remote, private.Public().(ed25519.PublicKey))
	if !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("canceled final QC recheck was reported successful: %v", err)
	}
	if verifier.calls != 2 || remote.requests != 1 {
		t.Fatalf("unexpected QC/remote path: verifier=%d remote=%d", verifier.calls, remote.requests)
	}
	if current, err := journal.Current(); err != nil || current != cp {
		t.Fatalf("fail-closed cancellation erased the durable local intent: current=%+v err=%v", current, err)
	}
}

// A QC source may cancel during its first check but still return nil.
// The remote fencing path must not publish a new fsynced intent, generate
// a challenged request, or contact a signer authority after that point.
// Unlike a remote outage AFTER fsync, this early cancellation must leave
// the existing durable checkpoint exactly unchanged.
func TestWitnessFenceRejectsCancellationDuringInitialQCBeforeFsync(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-cancel-first-qc", []byte("network")))
	root := types.HashBytes("witness-cancel-first-qc", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 47, HeaderHash: types.Hash{9}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := &witnessCancelOnRecheckVerifier{checkpoint: cp, cancel: cancel, cancelAt: 1}
	journal := witnessFenceTestJournal(t, cp, verifier)
	before, err := journal.Current()
	if err != nil {
		t.Fatalf("read initial intent: %v", err)
	}
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 37
	private := ed25519.NewKeyFromSeed(seed)
	remote := &witnessFenceAuthorityFixture{private: private}
	key := private.Public().(ed25519.PublicKey)
	err = ReserveAndVerifyWitnessFence(ctx, journal, cp, remote, key)
	if !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled initial QC was not fenced with context reason: %v", err)
	}
	if verifier.calls != 1 || remote.requests != 0 {
		t.Fatalf("first QC cancellation continued to remote: qc=%d requests=%d", verifier.calls, remote.requests)
	}
	after, err := journal.Current()
	if err != nil || after != before {
		t.Fatalf("canceled initial QC advanced durable intent: before=%+v after=%+v err=%v", before, after, err)
	}
	// Cancellation must release the exclusive journal mutex, allowing a
	// later independently verified request to durably reserve and reconcile.
	if err := ReserveAndVerifyWitnessFence(context.Background(), journal, cp, remote, key); err != nil {
		t.Fatalf("fresh valid request could not reserve after cancellation: %v", err)
	}
	current, err := journal.Current()
	if err != nil || current != cp || remote.requests != 1 {
		t.Fatalf("later valid reservation did not complete: cp=%+v requests=%d err=%v", current, remote.requests, err)
	}
}
