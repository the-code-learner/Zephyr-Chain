package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestObserveSettledRecordReplicatedMajority(t *testing.T) {
	workload := types.Hash{1}
	registry, err := NewWorkRegistry([]WorkSpec{{
		Version:       WorkSpecVersion,
		Class:         WorkAITraining,
		Units:         100,
		WorkloadHash:  workload,
		BenchmarkHash: types.Hash{2},
		Vector:        WorkVector{TensorUnits: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}

	jobID := types.JobID{3}
	providers := []types.AccountID{{4}, {5}, {6}}
	majorityRoot := types.Hash{7}
	record := OnChainJob{
		ID: jobID,
		Job: Job{
			Owner:              types.AccountID{8},
			WorkloadHash:       workload,
			InputRoot:          types.Hash{9},
			Resources:          Resources{CPUCores: 1, MemoryMiB: 1},
			MaxPrice:           60,
			CollateralRequired: 1,
			Verification:       VerificationReplicated,
			DeadlineHeight:     100,
			Replicas:           3,
		},
		Escrow: 60,
		Status: JobSettled,
		Assignments: []Assignment{
			{OfferID: types.Hash{10}, Provider: providers[0], Price: 10},
			{OfferID: types.Hash{11}, Provider: providers[1], Price: 20},
			{OfferID: types.Hash{12}, Provider: providers[2], Price: 30},
		},
		Results: []Result{
			{JobID: jobID, Provider: providers[0], ResultRoot: majorityRoot, CompletedHeight: 10},
			{JobID: jobID, Provider: providers[1], ResultRoot: majorityRoot, CompletedHeight: 10},
			{JobID: jobID, Provider: providers[2], ResultRoot: types.Hash{13}, CompletedHeight: 10},
		},
	}

	observed, err := ObserveSettledRecord(record, registry)
	if err != nil {
		t.Fatal(err)
	}
	if observed.PaidZPH != 30 {
		t.Fatalf("paid = %d, want 30", observed.PaidZPH)
	}
	if observed.ResultRoot != majorityRoot || observed.Units != 100 || observed.Class != WorkAITraining {
		t.Fatalf("unexpected verified work: %+v", observed)
	}
}

func TestObserveSettledRecordRejectsUnsettled(t *testing.T) {
	registry, err := NewWorkRegistry([]WorkSpec{{
		Version:       WorkSpecVersion,
		Class:         WorkCPUGeneral,
		Units:         1,
		WorkloadHash:  types.Hash{1},
		BenchmarkHash: types.Hash{2},
		Vector:        WorkVector{CPUUnits: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ObserveSettledRecord(OnChainJob{Status: JobPending}, registry)
	if err == nil {
		t.Fatal("expected unsettled record rejection")
	}
}
