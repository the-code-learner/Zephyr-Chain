package node

import (
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestGlobalCommitJournalRestoresShadowEconomicsAcrossEpochBoundary(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("global-journal-economics")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	store := worldstate.NewMemory()
	journalPath := filepath.Join(t.TempDir(), "global-commit.journal")

	runtime, engineConfig := newCheckpointEconomicsRuntime(t, network, native, validatorRoot, store)
	if err := runtime.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	commitEmptySchedulerBlock(t, runtime, key, validators, 1)

	restarted1, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted1.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := restarted1.RecoverGlobalCommitJournal(validators, nil, engineConfig); err != nil {
		t.Fatal(err)
	}
	metrics, _, err := restarted1.EconomicEpochSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].Epoch != 1 || metrics[0].ResourceCapacity != 100 {
		t.Fatalf("journal did not restore mid-epoch economics: %#v", metrics)
	}

	commitEmptySchedulerBlock(t, restarted1, key, validators, 2)
	pendingBefore, ok := restarted1.PendingEconomicState()
	if !ok || pendingBefore.Epoch != 1 || !pendingBefore.Shadow {
		t.Fatalf("epoch boundary did not produce pending shadow state: %#v", pendingBefore)
	}
	if _, exists := store.GetObject(economics.MonetaryStateObjectID(network)); exists {
		t.Fatal("pending economic state entered world state before next consensus candidate")
	}

	restarted2, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted2.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := restarted2.RecoverGlobalCommitJournal(validators, nil, engineConfig); err != nil {
		t.Fatal(err)
	}
	pendingAfter, ok := restarted2.PendingEconomicState()
	if !ok || pendingAfter != pendingBefore {
		t.Fatalf("pending shadow state changed across global journal restart: %#v != %#v", pendingAfter, pendingBefore)
	}

	candidate, err := restarted2.BuildCandidate(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidate(t, restarted2, key, validators, candidate)
	finalized, ok := restarted2.FinalizedEconomicState()
	if !ok || finalized != pendingBefore {
		t.Fatalf("restored pending economics did not finalize: %#v", finalized)
	}
	objectState, exists := store.GetObject(economics.MonetaryStateObjectID(network))
	if !exists || objectState.Version != 1 {
		t.Fatal("consensus-finalized monetary object missing after recovered epoch")
	}
}
