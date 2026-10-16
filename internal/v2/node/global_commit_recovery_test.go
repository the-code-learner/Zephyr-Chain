package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

type failBeforeApplyBackend struct {
	*worldstate.Memory
	fail bool
}

func (b *failBeforeApplyBackend) Apply(consumed []types.ObjectID, created []object.Object) (types.Hash, error) {
	if b.fail {
		return b.Root(), errors.New("simulated shard storage failure before apply")
	}
	return b.Memory.Apply(consumed, created)
}

func TestGlobalCommitJournalCompletesPartialMultiShardApply(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("global-recovery-partial")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	state0 := worldstate.NewMemory()
	state1 := &failBeforeApplyBackend{Memory: worldstate.NewMemory()}
	old0 := systemObjectForShard(0, 1, "recover-old-0")
	old1 := systemObjectForShard(1, 1, "recover-old-1")
	if _, err := state0.Apply(nil, []object.Object{old0}); err != nil {
		t.Fatal(err)
	}
	if _, err := state1.Memory.Apply(nil, []object.Object{old1}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: state0, 1: state1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(t.TempDir(), "global-commit.journal")
	if err := runtime.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	new0 := systemObjectForShard(0, 2, "recover-new-0")
	new1 := systemObjectForShard(1, 2, "recover-new-1")
	candidate := manualCandidateForDeltas(t, runtime, map[uint32]shardDelta{
		0: {Consumed: []types.ObjectID{old0.ID}, Created: []object.Object{new0}},
		1: {Consumed: []types.ObjectID{old1.ID}, Created: []object.Object{new1}},
	})
	state1.fail = true
	commitSchedulerCandidateExpectError(t, runtime, key, validators, candidate)
	if !runtime.RecoveryRequired() || runtime.Height != 0 {
		t.Fatal("partial global apply did not fail-stop at the pre-commit anchor")
	}
	intent, err := readGlobalCommitJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Status != globalCommitPreparing {
		t.Fatalf("partial apply journal status = %d, want PREPARING", intent.Status)
	}
	if _, ok := state0.GetObject(new0.ID); !ok {
		t.Fatal("first shard was not applied before injected second-shard failure")
	}
	if _, ok := state1.GetObject(old1.ID); !ok {
		t.Fatal("second shard unexpectedly mutated before injected failure")
	}

	state1.fail = false
	if err := runtime.RecoverGlobalCommitJournal(validators, nil, economics.ShadowEpochEngineConfig{}); err != nil {
		t.Fatal(err)
	}
	if runtime.RecoveryRequired() || runtime.Height != 1 {
		t.Fatalf("global recovery did not advance the certified commit: height=%d recovery=%v", runtime.Height, runtime.RecoveryRequired())
	}
	if _, ok := state1.GetObject(new1.ID); !ok {
		t.Fatal("recovery did not complete the missing shard transition")
	}
	intent, err = readGlobalCommitJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Status != globalCommitCommitted {
		t.Fatalf("recovered journal status = %d, want COMMITTED", intent.Status)
	}
	if _, err := runtime.BuildCandidate(2, nil); err != nil {
		t.Fatalf("runtime remained blocked after successful recovery: %v", err)
	}
}

func TestCommittedGlobalJournalRestoresRuntimeAnchorAfterRestart(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("global-recovery-committed")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	state := worldstate.NewMemory()
	oldObject := systemObjectForShard(0, 1, "restart-old")
	if _, err := state.Apply(nil, []object.Object{oldObject}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: state}, 1)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(t.TempDir(), "global-commit.journal")
	if err := runtime.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	candidate := manualCandidateForDeltas(t, runtime, map[uint32]shardDelta{
		0: {Consumed: []types.ObjectID{oldObject.ID}, Created: []object.Object{systemObjectForShard(0, 2, "restart-new")}},
	})
	commitSchedulerCandidate(t, runtime, key, validators, candidate)
	if runtime.Height != 1 {
		t.Fatal("initial certified commit did not advance")
	}
	info, err := os.Stat(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("global journal mode = %o, want 600", info.Mode().Perm())
	}

	restarted, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: state}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	if !restarted.RecoveryRequired() {
		t.Fatal("existing committed journal did not force explicit restart recovery")
	}
	if err := restarted.RecoverGlobalCommitJournal(validators, nil, economics.ShadowEpochEngineConfig{}); err != nil {
		t.Fatal(err)
	}
	if restarted.Height != 1 || restarted.ParentHash != runtime.ParentHash || restarted.RecoveryRequired() {
		t.Fatalf("committed journal did not restore runtime anchor: height=%d parent=%x", restarted.Height, restarted.ParentHash)
	}
	if _, err := restarted.BuildCandidate(2, nil); err != nil {
		t.Fatalf("restarted runtime cannot continue after journal recovery: %v", err)
	}
}
