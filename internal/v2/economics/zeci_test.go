package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestZECIShadowTelemetryCanonicalAcrossInputOrder(t *testing.T) {
	metrics := []ShardEpochMetrics{
		zeciMetric(0, 40, 100, 10, 20, 10, 0, 0, 100),
		zeciMetric(1, 60, 100, 5, 15, 5, 0, 0, 300),
	}
	work := []compute.VerifiedWork{
		zeciWork(1, 7, 11),
		zeciWork(2, 9, 13),
	}
	productive := &IdleCapitalSnapshot{
		Height:                    100,
		ProductiveObservedCapital: 500,
		ProductiveCoverageBps:     6_000,
	}
	a, err := BuildZECIShadowTelemetry(metrics, work, productive)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildZECIShadowTelemetry(
		[]ShardEpochMetrics{metrics[1], metrics[0]},
		[]compute.VerifiedWork{work[1], work[0]},
		productive,
	)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("ZECI shadow telemetry changed with input order: a=%+v b=%+v", a, b)
	}
	if !a.Shadow || !a.ProductiveCoverageAvailable || a.ProductiveCoverageBps != 6_000 {
		t.Fatalf("unexpected shadow/productive fields: %+v", a)
	}
	if a.ResourceUtilizationBps != 5_000 {
		t.Fatalf("resource utilization = %d, want 5000", a.ResourceUtilizationBps)
	}
	if a.AgeWeightedVelocityBps != 200 {
		t.Fatalf("supply-weighted velocity = %d, want 200", a.AgeWeightedVelocityBps)
	}
	if a.VerifiedWorkObservations != 2 || a.VerifiedWorkUnits != 16 || a.VerifiedWorkPaidZPH != 24 {
		t.Fatalf("unexpected verified-work totals: %+v", a)
	}
	hash, err := a.Hash()
	if err != nil || types.IsZero32([32]byte(hash)) {
		t.Fatalf("invalid ZECI hash: %v %s", err, hash)
	}
}

func TestZECIRejectsDuplicateVerifiedJob(t *testing.T) {
	metrics := []ShardEpochMetrics{zeciMetric(0, 10, 100, 0, 0, 0, 0, 0, 100)}
	work := zeciWork(1, 7, 11)
	if _, err := BuildZECIShadowTelemetry(metrics, []compute.VerifiedWork{work, work}, nil); err == nil {
		t.Fatal("duplicate verified job was accepted")
	}
}

func TestZECIWithoutProductiveCoverageStaysExplicitlyUnavailable(t *testing.T) {
	metrics := []ShardEpochMetrics{zeciMetric(0, 10, 100, 0, 0, 0, 0, 0, 100)}
	out, err := BuildZECIShadowTelemetry(metrics, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.ProductiveCoverageAvailable || out.ProductiveObservedCapital != 0 || out.ProductiveCoverageBps != 0 {
		t.Fatalf("missing productive telemetry was invented: %+v", out)
	}
}

func zeciMetric(shard uint32, used, capacity, demand, supply, fulfilled, expired, backlog uint64, velocity uint32) ShardEpochMetrics {
	return ShardEpochMetrics{
		Version:                   EpochMetricsVersion,
		Epoch:                     7,
		ShardID:                   shard,
		ResourceUsed:              used,
		ResourceCapacity:          capacity,
		CirculatingNativeSupply:   1_000,
		AgeWeightedVelocityBps:    velocity,
		EscrowBackedComputeDemand: demand,
		VerifiedComputeSupply:     supply,
		ComputeSupplyReliable:     true,
		ComputeFulfilled:          fulfilled,
		ComputeExpired:            expired,
		ComputeBacklog:            backlog,
	}
}

func zeciWork(seed byte, units, paid uint64) compute.VerifiedWork {
	return compute.VerifiedWork{
		JobID:        types.JobID{seed},
		Class:        compute.WorkCPUGeneral,
		Units:        units,
		Vector:       compute.WorkVector{CPUUnits: units},
		PaidZPH:      paid,
		Verification: compute.VerificationReplicated,
		ResultRoot:   types.Hash{seed + 10},
	}
}
