package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
)

func TestDeriveComputeCapitalComponentsSeparatesEscrowAndCollateral(t *testing.T) {
	record := finalizedComputeRecord(false)
	components, err := DeriveComputeCapitalComponents(record)
	if err != nil {
		t.Fatal(err)
	}
	if components.JobID != record.ID || components.Escrow.Amount != record.Escrow {
		t.Fatalf("unexpected escrow component set: %#v", components)
	}
	if components.TotalLocked != 120 || len(components.Collateral) != 2 {
		t.Fatalf("unexpected locked capital split: %#v", components)
	}
	if components.Escrow.ID == components.Collateral[0].ID ||
		components.Escrow.ID == components.Collateral[1].ID ||
		components.Collateral[0].ID == components.Collateral[1].ID {
		t.Fatal("compute capital component identities are not distinct")
	}

	updated := record
	updated.Status = compute.JobAwaitingVerification
	updated.Results = append([]compute.Result(nil), record.Results...)
	next, err := DeriveComputeCapitalComponents(updated)
	if err != nil {
		t.Fatal(err)
	}
	if next.Escrow.ID != components.Escrow.ID {
		t.Fatal("escrow component identity changed across job state update")
	}
	for i := range components.Collateral {
		if next.Collateral[i].ID != components.Collateral[i].ID {
			t.Fatal("collateral component identity changed across job state update")
		}
	}
}

func TestDeriveComputeCapitalComponentsRejectsDuplicateProvider(t *testing.T) {
	record := finalizedComputeRecord(false)
	record.Assignments = append(record.Assignments, record.Assignments[0])
	if _, err := DeriveComputeCapitalComponents(record); err == nil {
		t.Fatal("duplicate provider collateral component was accepted")
	}
}
