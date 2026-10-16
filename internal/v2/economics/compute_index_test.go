package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestComputeIndexUsesVerifiedSettlementsAndCoverage(t *testing.T) {
	cfg := ComputeIndexConfig{MinSamplesPerClass: 2, MinCoverageBps: 10_000, EWMABps: 10_000}
	cfg.WeightsBps[compute.WorkCPUGeneral] = 5_000
	cfg.WeightsBps[compute.WorkTensorAI] = 5_000
	observations := []compute.VerifiedWork{
		verifiedWork(compute.WorkCPUGeneral, 100, 200),
		verifiedWork(compute.WorkCPUGeneral, 100, 400),
		verifiedWork(compute.WorkTensorAI, 100, 600),
		verifiedWork(compute.WorkTensorAI, 100, 1_000),
	}
	snapshot, err := BuildComputeIndex(1, observations, ComputeIndexSnapshot{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Reliable || snapshot.CoverageBps != 10_000 || snapshot.TotalSamples != 4 {
		t.Fatalf("unexpected index coverage: %#v", snapshot)
	}
	// CPU median is 3 atomic ZPH/work unit; tensor median is 8.
	// With equal basket weights the deterministic midpoint is 5.5.
	if snapshot.ClassPriceQ9[compute.WorkCPUGeneral] != 3*PriceScaleQ9 ||
		snapshot.ClassPriceQ9[compute.WorkTensorAI] != 8*PriceScaleQ9 ||
		snapshot.BasketPriceQ9 != 11*PriceScaleQ9/2 {
		t.Fatalf("unexpected compute prices: %#v", snapshot)
	}
}

func TestComputeIndexRequiresEnoughVerifiedClasses(t *testing.T) {
	cfg := ComputeIndexConfig{MinSamplesPerClass: 2, MinCoverageBps: 7_500, EWMABps: 10_000}
	cfg.WeightsBps[compute.WorkCPUGeneral] = 5_000
	cfg.WeightsBps[compute.WorkTensorAI] = 5_000
	observations := []compute.VerifiedWork{
		verifiedWork(compute.WorkCPUGeneral, 100, 200),
		verifiedWork(compute.WorkCPUGeneral, 100, 300),
	}
	snapshot, err := BuildComputeIndex(1, observations, ComputeIndexSnapshot{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Reliable || snapshot.CoverageBps != 5_000 {
		t.Fatalf("under-covered index must remain shadow/unreliable: %#v", snapshot)
	}
}

func TestComputePriceTrendIsBounded(t *testing.T) {
	if got := ComputePriceTrendBps(300, 100); got != 10_000 {
		t.Fatalf("expected positive clamp, got %d", got)
	}
	if got := ComputePriceTrendBps(10, 100); got != -9_000 {
		t.Fatalf("unexpected negative trend %d", got)
	}
}

func verifiedWork(class compute.WorkClass, units, paid uint64) compute.VerifiedWork {
	var jobID types.JobID
	var root types.Hash
	jobID[0] = byte(class)
	root[0] = byte(class)
	return compute.VerifiedWork{
		JobID:        jobID,
		Class:        class,
		Units:        units,
		PaidZPH:      paid,
		Verification: compute.VerificationReplicated,
		ResultRoot:   root,
	}
}
