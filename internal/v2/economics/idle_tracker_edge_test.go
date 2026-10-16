package economics

import (
	"bytes"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestIdleCapitalTrackerRepeatedSelfCyclingPreservesDormancy(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	current := trackerObjectID("repeat-cycle-0")
	if err := tracker.BootstrapObject(current, 50_000, 7); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 128; i++ {
		next := trackerObjectIDWithIndex("repeat-cycle", i)
		if err := tracker.ApplyTransition([]types.ObjectID{current}, []CapitalTarget{{ObjectID: next, Amount: 50_000}}, 0); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		current = next
	}
	lots, ok := tracker.ObjectLots(current)
	if !ok || len(lots) != 1 || lots[0].Amount != 50_000 || lots[0].IdleSinceHeight != 7 {
		t.Fatalf("repeated self-cycling changed dormancy: %#v", lots)
	}
}

func TestIdleCapitalTrackerChangeOutputPreservesInputAge(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	input := trackerObjectID("change-input")
	if err := tracker.BootstrapObject(input, 1_000, 20); err != nil {
		t.Fatal(err)
	}
	payment := trackerObjectID("change-payment")
	change := trackerObjectID("change-output")
	if err := tracker.ApplyTransition([]types.ObjectID{input}, []CapitalTarget{
		{ObjectID: payment, Amount: 350},
		{ObjectID: change, Amount: 650},
	}, 0); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id     types.ObjectID
		amount uint64
	}{{payment, 350}, {change, 650}} {
		lots, ok := tracker.ObjectLots(tc.id)
		if !ok || len(lots) != 1 || lots[0].Amount != tc.amount || lots[0].IdleSinceHeight != 20 {
			t.Fatalf("change split reset age for %s: %#v", tc.id.String(), lots)
		}
	}
}

func TestIdleCapitalTrackerMixedAgeMergeKeepsBothAges(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	oldID := trackerObjectID("mixed-old")
	youngID := trackerObjectID("mixed-young")
	if err := tracker.BootstrapObject(oldID, 400, 10); err != nil {
		t.Fatal(err)
	}
	if err := tracker.BootstrapObject(youngID, 600, 90); err != nil {
		t.Fatal(err)
	}
	merged := trackerObjectID("mixed-merged")
	if err := tracker.ApplyTransition([]types.ObjectID{oldID, youngID}, []CapitalTarget{{ObjectID: merged, Amount: 1_000}}, 0); err != nil {
		t.Fatal(err)
	}
	lots, ok := tracker.ObjectLots(merged)
	if !ok || len(lots) != 2 {
		t.Fatalf("mixed-age merge lost lineage: %#v", lots)
	}
	var oldAmount, youngAmount uint64
	for _, lot := range lots {
		switch lot.IdleSinceHeight {
		case 10:
			oldAmount += lot.Amount
		case 90:
			youngAmount += lot.Amount
		default:
			t.Fatalf("merge invented idle height: %#v", lot)
		}
	}
	if oldAmount != 400 || youngAmount != 600 {
		t.Fatalf("merge changed age-weighted capital: old=%d young=%d lots=%#v", oldAmount, youngAmount, lots)
	}
}

func TestIdleCapitalTrackerSecondMaterializationDoesNotMutateState(t *testing.T) {
	tracker := NewIdleCapitalTracker()
	seed := trackerObjectID("materialize-seed")
	if err := tracker.BootstrapObject(seed, 777, 33); err != nil {
		t.Fatal(err)
	}
	transferID := types.HashBytes("idle-capital/test-transfer", []byte("materialize"))
	if err := tracker.ApplyTransition([]types.ObjectID{seed}, []CapitalTarget{{TransferID: transferID, Amount: 777}}, 0); err != nil {
		t.Fatal(err)
	}
	destination := trackerObjectID("materialize-destination")
	if err := tracker.MaterializeTransfer(transferID, destination); err != nil {
		t.Fatal(err)
	}
	before, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.MaterializeTransfer(transferID, trackerObjectID("materialize-second")); err == nil {
		t.Fatal("second materialization accepted")
	}
	after, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed second materialization mutated tracker")
	}
}
