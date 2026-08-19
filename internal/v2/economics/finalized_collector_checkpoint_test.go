package economics

import (
	"bytes"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestEpochCollectorCheckpointRoundTrip(t *testing.T) {
	registry, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version: compute.WorkSpecVersion, Class: compute.WorkCPUGeneral, Units: 100,
		WorkloadHash: types.Hash{1}, BenchmarkHash: types.Hash{2},
		Vector: compute.WorkVector{CPUUnits: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	collector, err := NewEpochCollector(EpochCollectorConfig{
		Epoch: 3, ShardCount: 2, NativeToken: types.TokenID{3},
		InitialCirculatingSupply: map[uint32]uint64{0: 900, 1: 100},
		OpeningComputeBacklog:    map[uint32]uint64{0: 50},
		ResourceCapacityPerBlock: map[uint32]uint64{0: 1_000, 1: 2_000},
		VelocityPolicy: VelocityPolicy{
			MinAgeBlocks: 2, FullWeightAgeBlocks: 20, MaxVelocityBps: 10_000,
		},
		FeePolicy:    CompatibilityFeePolicy(),
		WorkRegistry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	collector.lastHeight = 22
	first := collector.shards[0]
	first.fees = FeeAllocation{Total: 7, Burn: 7}
	first.operations = 9
	first.resourceUsed = 50
	first.resourceCapacity = 2_000
	first.newDemand = 100
	first.fulfilled = 80
	first.expired = 10
	first.closingBacklog = 60
	first.computeSupply = 100
	first.computeSupplyReliable = true
	first.velocity.weightedValueBps.SetUint64(123_456)
	first.velocity.observedSpends = 4
	first.velocity.eligibleSpends = 2
	first.velocity.unknownAgeSpends = 1
	first.velocity.freshSpends = 1
	second := collector.shards[1]
	second.resourceCapacity = 4_000
	collector.verifiedWork = []compute.VerifiedWork{{
		JobID: types.JobID{4}, Class: compute.WorkCPUGeneral, Units: 100,
		Vector: compute.WorkVector{CPUUnits: 100}, PaidZPH: 25,
		Verification: compute.VerificationReplicated, ResultRoot: types.Hash{5},
	}}

	raw, err := collector.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreEpochCollector(raw, registry)
	if err != nil {
		t.Fatal(err)
	}
	rawAgain, err := restored.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, rawAgain) {
		t.Fatal("collector checkpoint is not canonical across restore")
	}
	if restored.Epoch() != 3 || restored.lastHeight != 22 || restored.supply[0] != 900 || restored.supply[1] != 100 {
		t.Fatalf("restored collector lost identity/state: %#v", restored)
	}
	if restored.shards[0].velocity.weightedValueBps.Uint64() != 123_456 || len(restored.verifiedWork) != 1 {
		t.Fatalf("restored collector lost accumulated telemetry: %#v", restored.shards[0])
	}
}

func TestEpochCollectorCheckpointRejectsDifferentWorkRegistry(t *testing.T) {
	original, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version: compute.WorkSpecVersion, Class: compute.WorkCPUGeneral, Units: 100,
		WorkloadHash: types.Hash{1}, BenchmarkHash: types.Hash{2}, Vector: compute.WorkVector{CPUUnits: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	collector, err := NewEpochCollector(EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: types.TokenID{3},
		InitialCirculatingSupply: map[uint32]uint64{0: 1_000},
		ResourceCapacityPerBlock: map[uint32]uint64{0: 100},
		VelocityPolicy: VelocityPolicy{MinAgeBlocks: 1, FullWeightAgeBlocks: 10, MaxVelocityBps: 10_000},
		FeePolicy: CompatibilityFeePolicy(), WorkRegistry: original,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := collector.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version: compute.WorkSpecVersion, Class: compute.WorkCPUGeneral, Units: 101,
		WorkloadHash: types.Hash{1}, BenchmarkHash: types.Hash{2}, Vector: compute.WorkVector{CPUUnits: 101},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreEpochCollector(raw, changed); err == nil {
		t.Fatal("checkpoint restored with a different workload registry")
	}
}

func TestRuntimeCollectorCloneOwnsRegistryAndCapacityConfig(t *testing.T) {
	registry, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version: compute.WorkSpecVersion, Class: compute.WorkCPUGeneral, Units: 10,
		WorkloadHash: types.Hash{1}, BenchmarkHash: types.Hash{2}, Vector: compute.WorkVector{CPUUnits: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	capacity := map[uint32]uint64{0: 100}
	collector, err := NewEpochCollector(EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: types.TokenID{3},
		InitialCirculatingSupply: map[uint32]uint64{0: 1_000},
		ResourceCapacityPerBlock: capacity,
		VelocityPolicy: VelocityPolicy{MinAgeBlocks: 1, FullWeightAgeBlocks: 10, MaxVelocityBps: 10_000},
		FeePolicy: CompatibilityFeePolicy(), WorkRegistry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	owned := collector.Clone()
	if owned == nil {
		t.Fatal("collector clone failed")
	}
	capacity[0] = 999
	if err := registry.Register(compute.WorkSpec{
		Version: compute.WorkSpecVersion, Class: compute.WorkRendering, Units: 20,
		WorkloadHash: types.Hash{9}, BenchmarkHash: types.Hash{10}, Vector: compute.WorkVector{GPUFP32Units: 20},
	}); err != nil {
		t.Fatal(err)
	}
	if owned.config.ResourceCapacityPerBlock[0] != 100 {
		t.Fatal("external capacity map mutation leaked into runtime-owned collector")
	}
	if _, ok := owned.config.WorkRegistry.Resolve(types.Hash{9}); ok {
		t.Fatal("external registry mutation leaked into runtime-owned collector")
	}
}
