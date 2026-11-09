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
}

func (v *witnessCancelOnRecheckVerifier) VerifyFinalizedCheckpoint(cp FinalityCheckpoint) error {
	if cp != v.checkpoint {
		return ErrFinalityCheckpoint
	}
	v.calls++
	if v.calls == 2 {
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
	verifier := &witnessCancelOnRecheckVerifier{checkpoint: cp, cancel: cancel}
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
