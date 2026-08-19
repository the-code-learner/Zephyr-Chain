package node

import (
	"bytes"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestGlobalCommitJournalRoundTripAndTamperRejection(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("global-journal")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	state0 := worldstate.NewMemory()
	state1 := worldstate.NewMemory()
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: state0, 1: state1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	old0 := systemObjectForShard(0, 1, "journal-old-0")
	old1 := systemObjectForShard(1, 1, "journal-old-1")
	if _, err := state0.Apply(nil, []object.Object{old0}); err != nil {
		t.Fatal(err)
	}
	if _, err := state1.Apply(nil, []object.Object{old1}); err != nil {
		t.Fatal(err)
	}
	candidate := manualCandidateForDeltas(t, runtime, map[uint32]shardDelta{
		0: {Consumed: []types.ObjectID{old0.ID}, Created: []object.Object{systemObjectForShard(0, 2, "journal-new-0")}},
		1: {Consumed: []types.ObjectID{old1.ID}, Created: []object.Object{systemObjectForShard(1, 2, "journal-new-1")}},
	})
	certificate := schedulerCertificate(t, runtime, key, validators, candidate)
	commitments, _, err := runtime.preflightCandidateState(candidate)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := runtime.buildGlobalCommitIntent(candidate, certificate, commitments, []byte("economic-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encodeGlobalCommitJournal(intent)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := decodeGlobalCommitJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	rawAgain, err := encodeGlobalCommitJournal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, rawAgain) {
		t.Fatal("global commit journal is not canonical across round trip")
	}
	if parsed.Header != intent.Header || parsed.Certificate.Hash() != intent.Certificate.Hash() || len(parsed.Deltas) != 2 {
		t.Fatalf("global commit journal lost certified state: %#v", parsed)
	}

	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)/2] ^= 0xff
	if _, err := decodeGlobalCommitJournal(tampered); err == nil {
		t.Fatal("tampered global commit journal was accepted")
	}
}
