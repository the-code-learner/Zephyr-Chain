package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func finalizedProviderEvidenceSetFixture(t *testing.T, majority bool) []FinalizedProviderComputeEvidence {
	t.Helper()
	record := finalizedComputeRecord(majority)
	kind := tx.OpComputeFinalize
	if majority {
		kind = tx.OpComputeResolveReplicated
	}
	transaction, result := finalizedComputeTransaction(t, record, kind)
	evidence, applied, err := DeriveFinalizedProviderComputeEvidence(transaction, result, finalizedProviderWorkRegistry(t))
	if err != nil || !applied || len(evidence) != 2 {
		t.Fatalf("invalid provider fixture: len=%d applied=%v err=%v", len(evidence), applied, err)
	}
	return evidence
}

func TestHashFinalizedProviderComputeEvidenceSetCanonicalOrder(t *testing.T) {
	items := finalizedProviderEvidenceSetFixture(t, false)
	before := append([]FinalizedProviderComputeEvidence(nil), items...)
	got, err := HashFinalizedProviderComputeEvidenceSet(items)
	if err != nil || types.IsZero32([32]byte(got)) {
		t.Fatalf("set hash = %x, %v", got, err)
	}
	reversed := []FinalizedProviderComputeEvidence{items[1], items[0]}
	other, err := HashFinalizedProviderComputeEvidenceSet(reversed)
	if err != nil || other != got {
		t.Fatalf("reordered set hash = %x, %v; want %x", other, err, got)
	}
	if items[0] != before[0] || items[1] != before[1] {
		t.Fatal("input records were mutated")
	}
	changed := append([]FinalizedProviderComputeEvidence(nil), items...)
	changed[0].PaidZPH++
	hashChanged, err := HashFinalizedProviderComputeEvidenceSet(changed)
	if err != nil || hashChanged == got {
		t.Fatalf("payment did not bind hash: %x, %v", hashChanged, err)
	}
}

func TestHashFinalizedProviderComputeEvidenceSetFailsClosed(t *testing.T) {
	items := finalizedProviderEvidenceSetFixture(t, false)
	cases := map[string][]FinalizedProviderComputeEvidence{
		"nil":       nil,
		"empty":     {},
		"incomplete": {items[0]},
		"duplicate": {items[0], items[0]},
	}
	for name, records := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := HashFinalizedProviderComputeEvidenceSet(records); err == nil {
				t.Fatal("invalid provider evidence set was accepted")
			}
		})
	}

	tests := map[string]func([]FinalizedProviderComputeEvidence){
		"mixed job": func(v []FinalizedProviderComputeEvidence) {
			v[1].JobID = types.JobID{88}
		},
		"mixed receipt": func(v []FinalizedProviderComputeEvidence) {
			v[1].SettlementReceiptHash = types.Hash{89}
		},
		"mixed registry": func(v []FinalizedProviderComputeEvidence) {
			v[1].WorkRegistryHash = types.Hash{90}
		},
		"mixed result": func(v []FinalizedProviderComputeEvidence) {
			v[1].ResultRoot = types.Hash{91}
		},
		"mixed work vector": func(v []FinalizedProviderComputeEvidence) {
			v[1].Vector.CPUUnits++
		},
		"invalid provider": func(v []FinalizedProviderComputeEvidence) {
			v[1].Provider = types.AccountID{}
		},
		"mismatched declared count": func(v []FinalizedProviderComputeEvidence) {
			v[1].PaidProviderCount++
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			records := append([]FinalizedProviderComputeEvidence(nil), items...)
			mutate(records)
			if _, err := HashFinalizedProviderComputeEvidenceSet(records); err == nil {
				t.Fatal("tampered provider evidence set was accepted")
			}
		})
	}
}

func TestHashFinalizedProviderComputeEvidenceSetMajority(t *testing.T) {
	items := finalizedProviderEvidenceSetFixture(t, true)
	got, err := HashFinalizedProviderComputeEvidenceSet(items)
	if err != nil || types.IsZero32([32]byte(got)) {
		t.Fatalf("majority set hash = %x, %v", got, err)
	}
	for _, item := range items {
		if item.AssignedProviderCount != 3 || item.PaidProviderCount != 2 ||
			item.Provider == (types.AccountID{4}) {
			t.Fatal("slashed minority provider incorrectly included")
		}
	}
}
