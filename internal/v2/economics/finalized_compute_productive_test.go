package economics

import (
	"math"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDeriveFinalizedComputeProductiveEvidenceExactSettlement(t *testing.T) {
	record := finalizedComputeRecord(false)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeFinalize)

	evidence, applied, err := DeriveFinalizedComputeProductiveEvidence(transaction, result)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("finalized compute settlement was not recognized")
	}
	if evidence.JobID != record.ID || evidence.JobObjectID != transaction.Inputs[0].ObjectID {
		t.Fatalf("unexpected settlement identity: %#v", evidence)
	}
	if evidence.Escrow != 100 || evidence.Paid != 70 || evidence.CoverageBps != 7_000 {
		t.Fatalf("unexpected productive capital evidence: %#v", evidence)
	}
	if evidence.ResultRoot != types.Hash{9} || evidence.Verification != compute.VerificationReplicated {
		t.Fatalf("unexpected verification evidence: %#v", evidence)
	}
}

func TestDeriveFinalizedComputeProductiveEvidenceMajorityOnlyCountsPaidProviders(t *testing.T) {
	record := finalizedComputeRecord(true)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeResolveReplicated)

	evidence, applied, err := DeriveFinalizedComputeProductiveEvidence(transaction, result)
	if err != nil {
		t.Fatal(err)
	}
	if !applied || evidence.Paid != 60 || evidence.Escrow != 100 || evidence.CoverageBps != 6_000 {
		t.Fatalf("majority settlement did not isolate paid compute escrow: %#v", evidence)
	}
	if evidence.ResultRoot != types.Hash{9} {
		t.Fatalf("wrong accepted majority root: %x", evidence.ResultRoot)
	}
}

func TestDeriveFinalizedComputeProductiveEvidenceRejectsTamperedReceipt(t *testing.T) {
	record := finalizedComputeRecord(false)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeFinalize)
	receipt, err := compute.ParseSettlementReceipt(result.Created[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Payments[types.AccountID{2}]++
	result.Created[0].Data = receipt.MarshalBinary()

	if _, _, err := DeriveFinalizedComputeProductiveEvidence(transaction, result); err == nil {
		t.Fatal("tampered finalized settlement receipt was accepted")
	}
}

func TestDeriveFinalizedComputeProductiveEvidenceIgnoresNonSettlement(t *testing.T) {
	transaction := tx.Transaction{ShardID: 0, Operations: []tx.Operation{{Kind: tx.OpTransfer}}}
	result := execution.Result{TxID: transaction.ID()}
	evidence, applied, err := DeriveFinalizedComputeProductiveEvidence(transaction, result)
	if err != nil {
		t.Fatal(err)
	}
	if applied || evidence != (FinalizedComputeProductiveEvidence{}) {
		t.Fatalf("generic transfer received productive evidence: %#v", evidence)
	}
}

func TestProductiveCoverageBpsHandlesMaximumCapitalWithoutOverflow(t *testing.T) {
	coverage, err := productiveCoverageBps(math.MaxUint64-1, math.MaxUint64)
	if err != nil {
		t.Fatal(err)
	}
	if coverage != 9_999 {
		t.Fatalf("unexpected maximum-value coverage: %d", coverage)
	}
}

func finalizedComputeRecord(majority bool) compute.OnChainJob {
	owner := types.AccountID{1}
	providers := []types.AccountID{{2}, {3}}
	prices := []uint64{40, 30}
	roots := []types.Hash{{9}, {9}}
	replicas := uint16(2)
	if majority {
		providers = append(providers, types.AccountID{4})
		prices = []uint64{30, 30, 30}
		roots = []types.Hash{{9}, {9}, {8}}
		replicas = 3
	}
	jobID := types.JobID{7}
	assignments := make([]compute.Assignment, len(providers))
	results := make([]compute.Result, len(providers))
	for i, provider := range providers {
		assignments[i] = compute.Assignment{OfferID: types.Hash{byte(20 + i)}, Provider: provider, Price: prices[i]}
		results[i] = compute.Result{JobID: jobID, Provider: provider, ResultRoot: roots[i], CompletedHeight: 50}
	}
	return compute.OnChainJob{
		ID: jobID,
		Job: compute.Job{
			Owner: owner, WorkloadHash: types.Hash{5}, InputRoot: types.Hash{6},
			Resources: compute.Resources{CPUCores: 1, MemoryMiB: 256},
			MaxPrice: 100, CollateralRequired: 10, Verification: compute.VerificationReplicated,
			DeadlineHeight: 100, Replicas: replicas,
		},
		Escrow: 100, Status: compute.JobAwaitingVerification,
		Assignments: assignments, Results: results,
	}
}

func finalizedComputeTransaction(t *testing.T, record compute.OnChainJob, kind tx.OperationKind) (tx.Transaction, execution.Result) {
	t.Helper()
	jobRaw, err := record.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	jobObjectID := types.ObjectID{10}
	payload, err := (compute.JobRef{JobObject: jobObjectID}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0,
		Sender: record.Job.Owner,
		Inputs: []tx.InputRef{{ObjectID: jobObjectID}},
		Operations: []tx.Operation{{Kind: kind, Payload: payload}},
		Witnesses: []tx.Witness{{Object: object.Object{
			ID: jobObjectID, Version: 4, Owner: record.Job.Owner, Kind: object.KindComputeJob, Data: jobRaw,
		}}},
	}
	var settlement compute.OnChainSettlement
	if kind == tx.OpComputeFinalize {
		_, settlement, err = compute.FinalizeOnChain(record, compute.VerificationEvidence{})
	} else {
		_, settlement, err = compute.ResolveReplicatedMajority(record)
	}
	if err != nil {
		t.Fatal(err)
	}
	receipt := compute.SettlementReceipt{
		JobID: record.ID, ResultRoot: settlement.ResultRoot, Payments: settlement.Payments,
		Refund: settlement.Refund, Slashed: settlement.SlashedCollateral, SlashReward: settlement.SlashReward,
	}
	return transaction, execution.Result{
		Consumed: []types.ObjectID{jobObjectID},
		Created: []object.Object{{ID: types.ObjectID{11}, Version: 1, Kind: object.KindSystem, Data: receipt.MarshalBinary()}},
		TxID: transaction.ID(),
	}
}
