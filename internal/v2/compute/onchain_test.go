package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestOnChainReplicatedLifecycleAndCollateral(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("owner"))
	providers := []types.AccountID{
		types.AccountIDFromPublicKey([]byte("p1")),
		types.AccountIDFromPublicKey([]byte("p2")),
		types.AccountIDFromPublicKey([]byte("p3")),
	}
	resources := Resources{CPUCores: 4, MemoryMiB: 4096}
	job := Job{Owner: owner, WorkloadHash: types.HashBytes("work", []byte("render")), InputRoot: types.HashBytes("input", []byte("scene")), Resources: resources, MaxPrice: 30, CollateralRequired: 5, Verification: VerificationReplicated, DeadlineHeight: 100, Replicas: 3}
	record := OnChainJob{ID: types.JobID(types.HashBytes("job", []byte("one"))), Job: job, Escrow: 30, Status: JobPending}
	for i, provider := range providers {
		offer := Offer{Provider: provider, Resources: resources, PricePerUnit: uint64(i + 2), Collateral: 9, Verification: []VerificationMode{VerificationReplicated}, ValidUntilHeight: 90}
		var err error
		var excess uint64
		record, _, excess, err = AssignOnChain(record, types.HashBytes("offer", []byte{byte(i)}), offer, 10)
		if err != nil {
			t.Fatal(err)
		}
		if excess != 4 {
			t.Fatalf("unexpected collateral excess %d", excess)
		}
	}
	if record.Status != JobAssigned {
		t.Fatal("job not assigned after replica target")
	}
	root := types.HashBytes("result", []byte("same"))
	for i, provider := range providers {
		var err error
		record, err = SubmitOnChainResult(record, Result{JobID: record.ID, Provider: provider, ResultRoot: root, CompletedHeight: uint64(20 + i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	settled, settlement, err := FinalizeOnChain(record, VerificationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != JobSettled || settlement.ResultRoot != root || settlement.Refund != 21 {
		t.Fatalf("unexpected settlement: %+v", settlement)
	}
	for _, provider := range providers {
		if settlement.CollateralReturns[provider] != 5 {
			t.Fatal("provider collateral was not returned")
		}
	}
}

func TestReplicatedMajoritySlashesMinority(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("owner-majority"))
	providers := []types.AccountID{
		types.AccountIDFromPublicKey([]byte("mp1")),
		types.AccountIDFromPublicKey([]byte("mp2")),
		types.AccountIDFromPublicKey([]byte("mp3")),
	}
	resources := Resources{CPUCores: 2, MemoryMiB: 2048}
	job := Job{Owner: owner, WorkloadHash: types.HashBytes("work", []byte("majority")), InputRoot: types.HashBytes("input", []byte("majority")), Resources: resources, MaxPrice: 9, CollateralRequired: 7, Verification: VerificationReplicated, DeadlineHeight: 100, Replicas: 3}
	record := OnChainJob{ID: types.JobID(types.HashBytes("job", []byte("majority"))), Job: job, Escrow: 9, Status: JobAwaitingVerification}
	for i, provider := range providers {
		record.Assignments = append(record.Assignments, Assignment{OfferID: types.HashBytes("offer", []byte{byte(i)}), Provider: provider, Price: 2})
	}
	good := types.HashBytes("result", []byte("good"))
	bad := types.HashBytes("result", []byte("bad"))
	record.Results = []Result{
		{JobID: record.ID, Provider: providers[0], ResultRoot: good, CompletedHeight: 10},
		{JobID: record.ID, Provider: providers[1], ResultRoot: good, CompletedHeight: 10},
		{JobID: record.ID, Provider: providers[2], ResultRoot: bad, CompletedHeight: 10},
	}
	_, settlement, err := ResolveReplicatedMajority(record)
	if err != nil {
		t.Fatal(err)
	}
	if settlement.ResultRoot != good || settlement.SlashedCollateral[providers[2]] != 7 || settlement.SlashReward != 7 || settlement.Payments[providers[2]] != 0 {
		t.Fatalf("unexpected majority settlement: %+v", settlement)
	}
}
