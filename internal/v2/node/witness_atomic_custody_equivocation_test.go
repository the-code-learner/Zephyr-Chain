//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This intentionally faulty test verifier accepts two conflicting same-height
// claims. It models a failed custody-side QC dependency, NOT a valid verifier.
// Durable reservation must still prevent signing both values with one key.
type witnessFaultyCustodyVerifier struct{}

func (witnessFaultyCustodyVerifier) VerifyFinalizedCheckpoint(cp FinalityCheckpoint) error {
	return cp.Validate(cp.Network, cp.ValidatorRoot)
}

func TestAtomicWitnessCustodyStoreFencesEquivocationWhenVerifierCompromised(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("atomic-custody-equivocation", []byte("net")))
	root := types.HashBytes("atomic-custody-equivocation", []byte("root"))
	a := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 40, HeaderHash: types.Hash{3}}
	b := a
	b.HeaderHash[0]++
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "exclusive-custody-anchor")
	remote := openWitnessDurableCustodyFixture(t, path, a, true, witnessFaultyCustodyVerifier{}, authority, witness)
	j1 := witnessFenceTestJournal(t, a, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{a.Height: a.HeaderHash}})
	j2 := witnessFenceTestJournal(t, a, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{b.Height: b.HeaderHash}})
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	var wg sync.WaitGroup
	results := make([]WitnessAtomicCustodyAttestation, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j1, a, remote, keyA, keyW)
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j2, b, remote, keyA, keyW)
	}()
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("one custodian issued zero or two conflicting signatures: %v %v", errs[0], errs[1])
	}
	winner := a
	if errs[0] != nil {
		winner = b
		if !errors.Is(errs[0], ErrWitnessSigningIntent) {
			t.Fatalf("wrong error for rejected host: %v", errs[0])
		}
	} else if !errors.Is(errs[1], ErrWitnessSigningIntent) {
		t.Fatalf("wrong error for rejected host: %v", errs[1])
	}
	if errs[0] == nil && results[0].Checkpoint != a || errs[1] == nil && results[1].Checkpoint != b {
		t.Fatal("custody signature was issued for the wrong checkpoint")
	}
	stored, err := remote.store.Current()
	if err != nil || stored != winner {
		t.Fatalf("durable store disagrees with winning signature: stored=%+v winner=%+v err=%v", stored, winner, err)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
	remote = openWitnessDurableCustodyFixture(t, path, a, false, witnessFaultyCustodyVerifier{}, authority, witness)
	defer remote.Close()
	loser := a
	if winner == a {
		loser = b
	}
	if _, err := remote.ReserveAndSignWitnessCheckpoint(context.Background(), loser, [32]byte{1}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("conflicting signer succeeded after restart with faulty verifier: %v", err)
	}
	if got, err := remote.store.Current(); err != nil || got != winner {
		t.Fatalf("restart changed durable signing choice: got=%+v err=%v", got, err)
	}
}
