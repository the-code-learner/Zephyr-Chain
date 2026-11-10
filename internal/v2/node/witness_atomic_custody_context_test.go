//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// The test controls only a private entropy reader. It does not mutate
// crypto/rand.Reader, so parallel test workers and their actual signed
// challenges remain cryptographically secure and race-detector safe.
type witnessCancelDuringChallengeEntropy struct {
	cancel context.CancelFunc
	reads  int
}

func (e *witnessCancelDuringChallengeEntropy) Read(dst []byte) (int, error) {
	e.reads++
	for i := range dst {
		dst[i] = byte(i + 1)
	}
	e.cancel()
	return len(dst), nil
}

func TestAtomicWitnessCustodyCancelDuringChallengeNeverContactsSigner(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("witness-cancel-challenge", []byte("network")))
	root := types.HashBytes("witness-cancel-challenge", []byte("validator-root"))
	cp := FinalityCheckpoint{
		Network: network, ValidatorRoot: root, Height: 53, HeaderHash: types.Hash{7},
	}
	journal := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{
		allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash},
	})
	custodian := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	authorityKey := authority.Public().(ed25519.PublicKey)
	witnessKey := witness.Public().(ed25519.PublicKey)
	before, err := journal.Current()
	if err != nil {
		t.Fatalf("read initial local intent: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entropy := &witnessCancelDuringChallengeEntropy{cancel: cancel}
	_, err = reserveAndVerifyAtomicWitnessCheckpointWithEntropy(
		ctx, journal, cp, custodian, authorityKey, witnessKey, entropy,
	)
	if !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after entropy not preserved and fenced: %v", err)
	}
	if entropy.reads != 1 {
		t.Fatalf("challenge entropy read count=%d, want 1", entropy.reads)
	}
	if custodian.calls != 0 {
		t.Fatalf("canceled request still reached remote key custodian: %d", custodian.calls)
	}
	after, err := journal.Current()
	if err != nil || after != cp || after == before {
		t.Fatalf("fsynced intent was lost or replaced on cancellation: before=%+v after=%+v err=%v", before, after, err)
	}
	// Cancellation AFTER durable reservation never erases it; a fresh
	// independently verified caller may reconcile exactly that checkpoint.
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(
		context.Background(), journal, cp, custodian, authorityKey, witnessKey,
	); err != nil {
		t.Fatalf("new valid caller could not reconcile reserved intent: %v", err)
	}
	if custodian.calls != 1 {
		t.Fatalf("new valid caller did not issue exactly one remote request: %d", custodian.calls)
	}
}

// A broken or exhausted entropy source must not cause the node to emit an
// empty/truncated/reused challenge or delegate a request to the custodian.
func TestAtomicWitnessCustodyShortChallengeEntropyFailsClosed(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("witness-short-challenge", []byte("network")))
	root := types.HashBytes("witness-short-challenge", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 59, HeaderHash: types.Hash{4}}
	journal := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{
		allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash},
	})
	custodian := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	keysA := authority.Public().(ed25519.PublicKey)
	keysW := witness.Public().(ed25519.PublicKey)
	_, err := reserveAndVerifyAtomicWitnessCheckpointWithEntropy(
		context.Background(), journal, cp, custodian, keysA, keysW, io.LimitReader(&repeatingEntropyByteReader{value: 1}, 7),
	)
	if !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short entropy source not rejected: %v", err)
	}
	if custodian.calls != 0 {
		t.Fatalf("truncated challenge sent to remote custodian: %d", custodian.calls)
	}
	if current, err := journal.Current(); err != nil || current != cp {
		t.Fatalf("short challenge rolled back local durable intent: %+v %v", current, err)
	}
}

type repeatingEntropyByteReader struct {
	value byte
}

func (r *repeatingEntropyByteReader) Read(dst []byte) (int, error) {
	for i := range dst {
		dst[i] = r.value
	}
	return len(dst), nil
}
