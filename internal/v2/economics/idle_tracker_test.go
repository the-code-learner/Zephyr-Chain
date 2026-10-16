package economics

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestIdleCapitalTrackerSelfCycleAndFragmentationDoNotResetAge(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	seedID := trackerObjectID("seed")
	if err := tracker.BootstrapObject(seedID, 100_000, 10); err != nil {
		t.Fatal(err)
	}
	baseline, err := tracker.Snapshot(1_000, []uint64{10, 100, 1_000})
	if err != nil {
		t.Fatal(err)
	}

	targets := make([]CapitalTarget, 100)
	inputs := []types.ObjectID{seedID}
	for i := range targets {
		targets[i] = CapitalTarget{ObjectID: trackerObjectIDWithIndex("split", i), Amount: 1_000}
	}
	if err := tracker.ApplyTransition(inputs, targets, 0); err != nil {
		t.Fatal(err)
	}
	fragmented, err := tracker.Snapshot(1_000, []uint64{10, 100, 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fragmented.Dormancy, baseline.Dormancy) {
		t.Fatalf("wallet fragmentation reset dormancy: %#v != %#v", fragmented.Dormancy, baseline.Dormancy)
	}

	mergeInputs := make([]types.ObjectID, len(targets))
	for i, target := range targets {
		mergeInputs[i] = target.ObjectID
	}
	mergedID := trackerObjectID("merged")
	if err := tracker.ApplyTransition(mergeInputs, []CapitalTarget{{ObjectID: mergedID, Amount: 100_000}}, 0); err != nil {
		t.Fatal(err)
	}
	cycledID := trackerObjectID("cycled")
	if err := tracker.ApplyTransition([]types.ObjectID{mergedID}, []CapitalTarget{{ObjectID: cycledID, Amount: 100_000}}, 0); err != nil {
		t.Fatal(err)
	}
	lots, ok := tracker.ObjectLots(cycledID)
	if !ok || len(lots) != 1 || lots[0].IdleSinceHeight != 10 || lots[0].Amount != 100_000 {
		t.Fatalf("self-cycle changed prospective lineage: %#v", lots)
	}
}

func TestIdleCapitalTrackerTargetOrderDoesNotChangeCheckpoint(t *testing.T) {
	seedID := trackerObjectID("order-seed")
	left := NewIdleCapitalTracker()
	right := NewIdleCapitalTracker()
	for _, tracker := range []*IdleCapitalTracker{left, right} {
		if err := tracker.BootstrapObject(seedID, 1_000, 50); err != nil {
			t.Fatal(err)
		}
	}
	a := CapitalTarget{ObjectID: trackerObjectID("a"), Amount: 400}
	b := CapitalTarget{ObjectID: trackerObjectID("b"), Amount: 600}
	if err := left.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	if err := right.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{b, a}, 0); err != nil {
		t.Fatal(err)
	}
	leftRaw, err := left.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	rightRaw, err := right.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftRaw, rightRaw) {
		t.Fatal("target declaration order changed canonical tracker state")
	}
}

func TestIdleCapitalTrackerCrossShardPendingRoundTrip(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	seedID := trackerObjectID("cross-seed")
	if err := tracker.BootstrapObject(seedID, 10_000, 25); err != nil {
		t.Fatal(err)
	}
	transferID := types.HashBytes("idle-capital/test-transfer", []byte("one"))
	if err := tracker.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{{TransferID: transferID, Amount: 9_900}}, 100); err != nil {
		t.Fatal(err)
	}
	pending, err := tracker.Snapshot(1_000, []uint64{100, 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if pending.TrackedCapital != 9_900 || pending.PendingTransfers != 1 || pending.LiveObjects != 0 {
		t.Fatalf("unexpected pending cross-shard snapshot: %#v", pending)
	}
	importedID := trackerObjectID("cross-import")
	if err := tracker.MaterializeTransfer(transferID, importedID); err != nil {
		t.Fatal(err)
	}
	lots, ok := tracker.ObjectLots(importedID)
	if !ok || len(lots) != 1 || lots[0].IdleSinceHeight != 25 || lots[0].Amount != 9_900 {
		t.Fatalf("cross-shard import lost lineage: %#v", lots)
	}
	imported, err := tracker.Snapshot(1_000, []uint64{100, 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(imported.Dormancy, pending.Dormancy) || imported.PendingTransfers != 0 || imported.LiveObjects != 1 {
		t.Fatalf("materialization changed economic age: pending=%#v imported=%#v", pending, imported)
	}
}

func TestIdleCapitalTrackerProductiveHookIsExplicitAndWeighted(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	seedID := trackerObjectID("productive-seed")
	if err := tracker.BootstrapObject(seedID, 8_000, 10); err != nil {
		t.Fatal(err)
	}
	// Ordinary movement does not count as productive.
	movedID := trackerObjectID("ordinary-move")
	if err := tracker.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{{ObjectID: movedID, Amount: 8_000}}, 0); err != nil {
		t.Fatal(err)
	}
	before, err := tracker.Snapshot(100, []uint64{20, 100})
	if err != nil {
		t.Fatal(err)
	}
	if before.ProductiveObservedCapital != 0 || before.ProductiveCoverageBps != 0 {
		t.Fatalf("ordinary transfer fabricated productive coverage: %#v", before)
	}
	if err := tracker.MarkObjectProductive(movedID, 100, 2_500); err != nil {
		t.Fatal(err)
	}
	after, err := tracker.Snapshot(100, []uint64{20, 100})
	if err != nil {
		t.Fatal(err)
	}
	if after.ProductiveObservedCapital != 8_000 || after.ProductiveCoverageBps != 2_500 {
		t.Fatalf("unexpected productive coverage: %#v", after)
	}
	lots, _ := tracker.ObjectLots(movedID)
	var reset, idle uint64
	for _, lot := range lots {
		if lot.IdleSinceHeight == 100 {
			reset += lot.Amount
		} else if lot.IdleSinceHeight == 10 {
			idle += lot.Amount
		}
	}
	if reset != 2_000 || idle != 6_000 {
		t.Fatalf("productive hook reset wrong amount: reset=%d idle=%d lots=%#v", reset, idle, lots)
	}
}

func TestIdleCapitalTrackerCheckpointRoundTrip(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	seedID := trackerObjectID("checkpoint-seed")
	if err := tracker.BootstrapObject(seedID, 5_000, 12); err != nil {
		t.Fatal(err)
	}
	transferID := types.HashBytes("idle-capital/test-transfer", []byte("checkpoint"))
	localID := trackerObjectID("checkpoint-local")
	if err := tracker.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{
		{TransferID: transferID, Amount: 2_000},
		{ObjectID: localID, Amount: 3_000},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if err := tracker.MarkObjectProductive(localID, 100, 5_000); err != nil {
		t.Fatal(err)
	}
	raw, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreIdleCapitalTracker(raw)
	if err != nil {
		t.Fatal(err)
	}
	rawAgain, err := restored.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, rawAgain) {
		t.Fatal("idle tracker checkpoint is not canonical across restore")
	}
	before, err := tracker.Snapshot(200, []uint64{50, 200})
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.Snapshot(200, []uint64{50, 200})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("restored tracker changed state: %#v != %#v", after, before)
	}
}

func TestIdleCapitalTrackerRejectsNonConservingOrAmbiguousTransitions(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	seedID := trackerObjectID("invalid-seed")
	if err := tracker.BootstrapObject(seedID, 100, 10); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{{ObjectID: trackerObjectID("short"), Amount: 99}}, 0); err == nil {
		t.Fatal("non-conserving transition accepted")
	}
	both := CapitalTarget{ObjectID: trackerObjectID("both"), TransferID: types.Hash{1}, Amount: 100}
	if err := tracker.ApplyTransition([]types.ObjectID{seedID}, []CapitalTarget{both}, 0); err == nil {
		t.Fatal("ambiguous local/cross-shard target accepted")
	}
	if _, ok := tracker.ObjectLots(seedID); !ok {
		t.Fatal("failed transition mutated tracker")
	}
}

func trackerObjectID(label string) types.ObjectID {
	return types.ObjectID(types.HashBytes("idle-capital/test-object", []byte(label)))
}

func trackerObjectIDWithIndex(label string, index int) types.ObjectID {
	payload := append([]byte(label), byte(index>>24), byte(index>>16), byte(index>>8), byte(index))
	return types.ObjectID(types.HashBytes("idle-capital/test-object", payload))
}
