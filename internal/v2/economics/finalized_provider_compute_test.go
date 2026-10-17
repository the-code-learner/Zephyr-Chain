package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDeriveFinalizedProviderComputeEvidenceExactSettlement(t *testing.T) {
	record := finalizedComputeRecord(false)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeFinalize)
	registry := finalizedProviderWorkRegistry(t)

	evidence, applied, err := DeriveFinalizedProviderComputeEvidence(transaction, result, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !applied || len(evidence) != 2 {
		t.Fatalf("provider evidence = %#v applied=%v", evidence, applied)
	}
	if evidence[0].Provider != (types.AccountID{2}) || evidence[0].PaidZPH != 40 ||
		evidence[1].Provider != (types.AccountID{3}) || evidence[1].PaidZPH != 30 {
		t.Fatalf("unexpected provider ordering/payments: %#v", evidence)
	}
	for _, item := range evidence {
		if item.JobID != record.ID || item.JobOwner != record.Job.Owner ||
			item.WorkloadHash != record.Job.WorkloadHash ||
			item.BenchmarkHash != (types.Hash{6}) ||
			item.Class != compute.WorkCPUGeneral ||
			item.Units != 7 ||
			item.Vector != (compute.WorkVector{CPUUnits: 7}) ||
			item.Verification != compute.VerificationReplicated ||
			item.ResultRoot != (types.Hash{9}) ||
			item.AssignedProviderCount != 2 ||
			item.PaidProviderCount != 2 {
			t.Fatalf("unexpected provider evidence: %#v", item)
		}
		if types.IsZero32([32]byte(item.JobStateHash)) ||
			types.IsZero32([32]byte(item.SettlementReceiptHash)) ||
			types.IsZero32([32]byte(item.WorkRegistryHash)) {
			t.Fatalf("missing evidence commitments: %#v", item)
		}
		if hash, err := item.Hash(); err != nil || types.IsZero32([32]byte(hash)) {
			t.Fatalf("invalid provider evidence hash: %v %x", err, hash)
		}
	}
	if first, _ := evidence[0].Hash(); first == func() types.Hash {
		second, _ := evidence[1].Hash()
		return second
	}() {
		t.Fatal("provider identity did not bind evidence hash")
	}
}

func TestDeriveFinalizedProviderComputeEvidenceMajorityExcludesMinority(t *testing.T) {
	record := finalizedComputeRecord(true)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeResolveReplicated)
	registry := finalizedProviderWorkRegistry(t)

	evidence, applied, err := DeriveFinalizedProviderComputeEvidence(transaction, result, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !applied || len(evidence) != 2 {
		t.Fatalf("majority provider evidence = %#v applied=%v", evidence, applied)
	}
	for _, item := range evidence {
		if item.Provider == (types.AccountID{4}) {
			t.Fatal("minority/slashed provider received accepted paid-work evidence")
		}
		if item.PaidZPH != 30 || item.ResultRoot != (types.Hash{9}) ||
			item.AssignedProviderCount != 3 || item.PaidProviderCount != 2 {
			t.Fatalf("unexpected majority evidence: %#v", item)
		}
	}
}

func TestDeriveFinalizedProviderComputeEvidenceRejectsRegistryMismatch(t *testing.T) {
	record := finalizedComputeRecord(false)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeFinalize)
	wrong, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version:       compute.WorkSpecVersion,
		Class:         compute.WorkCPUGeneral,
		Units:         8,
		WorkloadHash:  record.Job.WorkloadHash,
		BenchmarkHash: types.Hash{6},
		Vector:        compute.WorkVector{CPUUnits: 8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DeriveFinalizedProviderComputeEvidence(transaction, result, wrong); err == nil {
		t.Fatal("settlement was attributed using a mismatched WorkRegistry spec")
	}
}

func TestDeriveFinalizedProviderComputeEvidenceRejectsTamperedReceipt(t *testing.T) {
	record := finalizedComputeRecord(false)
	transaction, result := finalizedComputeTransaction(t, record, tx.OpComputeFinalize)
	registry := finalizedProviderWorkRegistry(t)

	receipt, err := compute.ParseSettlementReceipt(result.Created[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Payments[types.AccountID{2}]++
	result.Created[0].Data = receipt.MarshalBinary()

	if _, _, err := DeriveFinalizedProviderComputeEvidence(transaction, result, registry); err == nil {
		t.Fatal("tampered settlement receipt produced provider evidence")
	}
}

func TestDeriveFinalizedProviderComputeEvidenceIgnoresNonSettlement(t *testing.T) {
	transaction := tx.Transaction{ShardID: 0, Operations: []tx.Operation{{Kind: tx.OpTransfer}}}
	result := finalizedProviderEmptyResult(transaction)
	registry := finalizedProviderWorkRegistry(t)

	evidence, applied, err := DeriveFinalizedProviderComputeEvidence(transaction, result, registry)
	if err != nil {
		t.Fatal(err)
	}
	if applied || len(evidence) != 0 {
		t.Fatalf("generic transfer produced provider compute evidence: %#v", evidence)
	}
}

func finalizedProviderWorkRegistry(t *testing.T) *compute.WorkRegistry {
	t.Helper()
	registry, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version:       compute.WorkSpecVersion,
		Class:         compute.WorkCPUGeneral,
		Units:         7,
		WorkloadHash:  types.Hash{5},
		BenchmarkHash: types.Hash{6},
		Vector:        compute.WorkVector{CPUUnits: 7},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func finalizedProviderEmptyResult(transaction tx.Transaction) execution.Result {
	return execution.Result{TxID: transaction.ID()}
}
