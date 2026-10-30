//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type witnessIntentTestVerifier struct {
	allowed map[uint64]types.Hash
}

func (v witnessIntentTestVerifier) VerifyFinalizedCheckpoint(cp FinalityCheckpoint) error {
	if cp.Validate(cp.Network, cp.ValidatorRoot) != nil || v.allowed[cp.Height] != cp.HeaderHash {
		return ErrFinalityCheckpoint
	}
	return nil
}

func TestWitnessIntentDurableRestartAndConflicts(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-intent", []byte("network")))
	root := types.HashBytes("witness-intent", []byte("genesis"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 7, HeaderHash: types.Hash{3}}
	second := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 8, HeaderHash: types.Hash{4}}
	conflict := second
	conflict.HeaderHash = types.Hash{9}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "witness-intents")
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{7: first.HeaderHash, 8: second.HeaderHash}}
	if _, err := OpenWitnessSigningIntentJournal(path, network, root, true, nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("missing verifier accepted: %v", err)
	}
	j, err := OpenWitnessSigningIntentJournal(path, network, root, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.ReserveFinalizedIntent(first); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWitnessSigningIntentJournal(path, network, root, false, verifier); err == nil {
		t.Fatal("second simultaneous journal writer allowed")
	}
	if err := j.ReserveFinalizedIntent(first); err != nil {
		t.Fatalf("same checkpoint should be idempotent: %v", err)
	}
	if err := j.ReserveFinalizedIntent(conflict); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("unverified finality accepted: %v", err)
	}
	if err := j.ReserveFinalizedIntent(second); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.ReserveFinalizedIntent(second); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("closed store accepted intent: %v", err)
	}
	j, err = OpenWitnessSigningIntentJournal(path, network, root, false, verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	cp, err := j.Current()
	if err != nil || cp != second {
		t.Fatalf("durable checkpoint not restored: %+v %v", cp, err)
	}
	if err := j.ReserveFinalizedIntent(first); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("rollback accepted: %v", err)
	}
	// Even if a compromised verifier asserts a contradictory hash, durable
	// intent storage must refuse a second value for the same height.
	badVerifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{8: conflict.HeaderHash}}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	k, err := OpenWitnessSigningIntentJournal(path, network, root, false, badVerifier)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.ReserveFinalizedIntent(conflict); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("equal-height equivocation accepted: %v", err)
	}
}

func TestWitnessIntentTamperingAndForeignChain(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-intent", []byte("net2")))
	root := types.HashBytes("witness-intent", []byte("root2"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 1, HeaderHash: types.Hash{8}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{1: cp.HeaderHash}}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "intent")
	j, err := OpenWitnessSigningIntentJournal(path, network, root, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.ReserveFinalizedIntent(cp); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWitnessSigningIntentJournal(path, types.NetworkID{2}, root, false, verifier); err == nil {
		t.Fatal("foreign network journal accepted")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0x7f}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWitnessSigningIntentJournal(path, network, root, false, verifier); err == nil {
		t.Fatal("torn journal accepted")
	}
}
