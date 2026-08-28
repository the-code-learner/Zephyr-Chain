package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
)

func TestBuildZCUReferenceUsesWeightedMedianAndRateLimit(t *testing.T) {
	cfg := ZCUReferenceConfig{MinSlotsPerClass: 2, EWMABps: BasisPoints, MaxEpochChangeBps: 500}
	prior := ZCUReferenceSnapshot{Epoch: 9}
	prior.ClassReferenceQ9[compute.WorkGPUFP32] = ZCUReferenceScaleQ9

	slots := []VerifiedComputeSlot{
		{Class: compute.WorkGPUFP32, PerformanceQ9: 900_000_000, DeliveredSlotTime: 10, AvailabilityEWMABps: 10_000, SuccessEWMABps: 10_000, ConfidenceBps: 10_000},
		{Class: compute.WorkGPUFP32, PerformanceQ9: 1_100_000_000, DeliveredSlotTime: 100, AvailabilityEWMABps: 10_000, SuccessEWMABps: 10_000, ConfidenceBps: 10_000},
		{Class: compute.WorkGPUFP32, PerformanceQ9: 5_000_000_000, DeliveredSlotTime: 1, AvailabilityEWMABps: 10_000, SuccessEWMABps: 10_000, ConfidenceBps: 1_000},
	}

	snapshot, err := BuildZCUReference(10, slots, prior, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ClassReliable[compute.WorkGPUFP32] {
		t.Fatal("expected GPU reference to be reliable")
	}
	if got, want := snapshot.ClassReferenceQ9[compute.WorkGPUFP32], uint64(1_050_000_000); got != want {
		t.Fatalf("reference = %d, want %d", got, want)
	}
}

func TestBuildZCUReferenceRequiresVerifiedCoverage(t *testing.T) {
	cfg := ZCUReferenceConfig{MinSlotsPerClass: 2, EWMABps: 5_000, MaxEpochChangeBps: 100}
	snapshot, err := BuildZCUReference(1, []VerifiedComputeSlot{{
		Class: compute.WorkCPUGeneral, PerformanceQ9: ZCUReferenceScaleQ9, DeliveredSlotTime: 10,
		AvailabilityEWMABps: 10_000, SuccessEWMABps: 10_000, ConfidenceBps: 10_000,
	}}, ZCUReferenceSnapshot{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ClassReliable[compute.WorkCPUGeneral] || snapshot.ClassReferenceQ9[compute.WorkCPUGeneral] != 0 {
		t.Fatal("single slot must not establish a reliable reference")
	}
}

func TestBuildZCUReferenceRejectsInvalidEvidence(t *testing.T) {
	_, err := BuildZCUReference(1, []VerifiedComputeSlot{{
		Class: compute.WorkGPUFP64, PerformanceQ9: ZCUReferenceScaleQ9, DeliveredSlotTime: 1,
		AvailabilityEWMABps: 10_001, SuccessEWMABps: 10_000, ConfidenceBps: 10_000,
	}}, ZCUReferenceSnapshot{}, ZCUReferenceConfig{MinSlotsPerClass: 1, EWMABps: 10_000, MaxEpochChangeBps: 100})
	if err == nil {
		t.Fatal("expected invalid availability evidence to fail")
	}
}
