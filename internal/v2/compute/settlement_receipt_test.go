package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestSettlementReceiptRoundTrip(t *testing.T) {
	receipt := SettlementReceipt{
		JobID:       types.JobID{1},
		ResultRoot:  types.Hash{2},
		Payments:    map[types.AccountID]uint64{{3}: 10, {4}: 20},
		Refund:      5,
		Slashed:     map[types.AccountID]uint64{{5}: 7},
		SlashReward: 7,
	}
	parsed, err := ParseSettlementReceipt(receipt.MarshalBinary())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.JobID != receipt.JobID || parsed.ResultRoot != receipt.ResultRoot || parsed.Refund != receipt.Refund || parsed.SlashReward != receipt.SlashReward {
		t.Fatalf("round trip mismatch: %+v", parsed)
	}
	paid, err := SettlementPaid(parsed)
	if err != nil || paid != 30 {
		t.Fatalf("paid = %d err=%v, want 30", paid, err)
	}
}

func TestObserveFinalizedSettlementReplicatedMajority(t *testing.T) {
	workload := types.Hash{11}
	registry, err := NewWorkRegistry([]WorkSpec{{
		Version:       WorkSpecVersion,
		Class:         WorkRendering,
		Units:         50,
		WorkloadHash:  workload,
		BenchmarkHash: types.Hash{12},
		Vector:        WorkVector{GPUFP32Units: 50},
	}})
	if err != nil {
		t.Fatal(err)
	}
	jobID := types.JobID{13}
	providers := []types.AccountID{{14}, {15}, {16}}
	root := types.Hash{17}
	record := OnChainJob{
		ID: jobID,
		Job: Job{
			Owner:              types.AccountID{18},
			WorkloadHash:       workload,
			InputRoot:          types.Hash{19},
			Resources:          Resources{GPUCount: 1, MemoryMiB: 1},
			MaxPrice:           60,
			CollateralRequired: 4,
			Verification:       VerificationReplicated,
			DeadlineHeight:     100,
			Replicas:           3,
		},
		Escrow: 60,
		Status: JobAwaitingVerification,
		Assignments: []Assignment{
			{OfferID: types.Hash{20}, Provider: providers[0], Price: 10},
			{OfferID: types.Hash{21}, Provider: providers[1], Price: 20},
			{OfferID: types.Hash{22}, Provider: providers[2], Price: 30},
		},
		Results: []Result{
			{JobID: jobID, Provider: providers[0], ResultRoot: root, CompletedHeight: 10},
			{JobID: jobID, Provider: providers[1], ResultRoot: root, CompletedHeight: 10},
			{JobID: jobID, Provider: providers[2], ResultRoot: types.Hash{23}, CompletedHeight: 10},
		},
	}
	receipt := SettlementReceipt{
		JobID:       jobID,
		ResultRoot:  root,
		Payments:    map[types.AccountID]uint64{providers[0]: 10, providers[1]: 20},
		Refund:      30,
		Slashed:     map[types.AccountID]uint64{providers[2]: 4},
		SlashReward: 4,
	}
	observed, err := ObserveFinalizedSettlement(record, receipt, registry)
	if err != nil {
		t.Fatal(err)
	}
	if observed.PaidZPH != 30 || observed.Units != 50 || observed.ResultRoot != root {
		t.Fatalf("unexpected observation: %+v", observed)
	}

	tampered := receipt
	tampered.Refund++
	if _, err := ObserveFinalizedSettlement(record, tampered, registry); err == nil {
		t.Fatal("expected tampered receipt rejection")
	}
}
