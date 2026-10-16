package economics

import (
	"reflect"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestWalletFragmentationPreservesDormancyHistogram(t *testing.T) {
	seed := CapitalLot{
		LineageID:       types.HashBytes("idle-lineage", []byte("seed")),
		Amount:          1_000_000,
		IdleSinceHeight: 10,
	}
	height := uint64(10_010)
	bounds := []uint64{100, 1_000, 10_000, 100_000}
	policy := StateCarryingCostPolicy{BaseUnitsPerObject: 8, UnitsPerKiB: 3}

	baseline, err := BuildDormancyHistogram([]CapitalLot{seed}, height, bounds)
	if err != nil {
		t.Fatal(err)
	}
	var previousCost uint64
	for _, fragments := range []uint32{10, 100, 1_000, 10_000} {
		scenario, err := SimulateWalletFragmentation(seed, fragments, height, 96, bounds, policy)
		if err != nil {
			t.Fatalf("%d fragments: %v", fragments, err)
		}
		if !reflect.DeepEqual(scenario.Histogram, baseline) {
			t.Fatalf("%d-way split changed dormancy histogram: %#v != %#v", fragments, scenario.Histogram, baseline)
		}
		if scenario.CarryingCostUnits <= previousCost {
			t.Fatalf("%d-way split did not increase carrying cost: %d <= %d", fragments, scenario.CarryingCostUnits, previousCost)
		}
		previousCost = scenario.CarryingCostUnits
	}
}

func TestMarkProductiveCoverageIsFragmentationStable(t *testing.T) {
	seed := CapitalLot{
		LineageID:       types.HashBytes("idle-lineage", []byte("productive")),
		Amount:          100_000,
		IdleSinceHeight: 100,
	}
	height := uint64(1_000)
	marked, err := MarkProductiveCoverage([]CapitalLot{seed}, height, 2_500)
	if err != nil {
		t.Fatal(err)
	}
	split, err := SplitCapitalLineage([]CapitalLot{seed}, equalAmounts(seed.Amount, 100))
	if err != nil {
		t.Fatal(err)
	}
	var fragmented []CapitalLot
	for _, lots := range split {
		fragmented = append(fragmented, lots...)
	}
	markedFragmented, err := MarkProductiveCoverage(fragmented, height, 2_500)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(markedFragmented, marked) {
		t.Fatalf("productive marking changed after wallet split: %#v != %#v", markedFragmented, marked)
	}
	if len(marked) != 2 {
		t.Fatalf("marked lots = %d, want 2", len(marked))
	}
	var reset, stillIdle uint64
	for _, lot := range marked {
		switch lot.IdleSinceHeight {
		case height:
			reset += lot.Amount
		case seed.IdleSinceHeight:
			stillIdle += lot.Amount
		default:
			t.Fatalf("unexpected idle height %d", lot.IdleSinceHeight)
		}
	}
	if reset != 25_000 || stillIdle != 75_000 {
		t.Fatalf("productive split = reset %d idle %d", reset, stillIdle)
	}
}

func TestProductiveCoverageAccumulatorUsesCapitalWeight(t *testing.T) {
	var acc ProductiveCoverageAccumulator
	if err := acc.Observe(1_000, 2_500); err != nil {
		t.Fatal(err)
	}
	if err := acc.Observe(3_000, 5_000); err != nil {
		t.Fatal(err)
	}
	snapshot, err := acc.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ObservedCapital != 4_000 || snapshot.ProductiveCoverageBps != 4_375 {
		t.Fatalf("unexpected productive coverage snapshot: %#v", snapshot)
	}
}

func TestSplitCapitalLineagePreservesMultipleCohorts(t *testing.T) {
	lineage := types.HashBytes("idle-lineage", []byte("cohorts"))
	lots := []CapitalLot{
		{LineageID: lineage, Amount: 600, IdleSinceHeight: 10},
		{LineageID: lineage, Amount: 400, IdleSinceHeight: 100},
	}
	split, err := SplitCapitalLineage(lots, []uint64{250, 250, 250, 250})
	if err != nil {
		t.Fatal(err)
	}
	var flattened []CapitalLot
	for _, output := range split {
		flattened = append(flattened, output...)
	}
	before, err := BuildDormancyHistogram(lots, 1_000, []uint64{500, 950})
	if err != nil {
		t.Fatal(err)
	}
	after, err := BuildDormancyHistogram(flattened, 1_000, []uint64{500, 950})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("cohort split changed histogram: %#v != %#v", after, before)
	}
}

func TestIdleCapitalRejectsManipulativeInputs(t *testing.T) {
	lineage := types.HashBytes("idle-lineage", []byte("invalid"))
	seed := CapitalLot{LineageID: lineage, Amount: 100, IdleSinceHeight: 10}
	if _, err := SplitCapitalLineage([]CapitalLot{seed}, []uint64{99}); err == nil {
		t.Fatal("non-conserving lineage split accepted")
	}
	if _, err := MarkProductiveCoverage([]CapitalLot{seed}, 100, 10_001); err == nil {
		t.Fatal("coverage above 100% accepted")
	}
	if _, err := BuildDormancyHistogram([]CapitalLot{seed}, 100, []uint64{100, 99}); err == nil {
		t.Fatal("non-monotonic dormancy buckets accepted")
	}
	if _, err := SimulateWalletFragmentation(seed, 101, 100, 64, []uint64{10}, StateCarryingCostPolicy{BaseUnitsPerObject: 1, UnitsPerKiB: 1}); err == nil {
		t.Fatal("fragment count above indivisible amount accepted")
	}
}

func equalAmounts(total uint64, count int) []uint64 {
	out := make([]uint64, count)
	base := total / uint64(count)
	remainder := total % uint64(count)
	for i := range out {
		out[i] = base
		if uint64(i) < remainder {
			out[i]++
		}
	}
	return out
}
