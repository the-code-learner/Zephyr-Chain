package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestReplicatedComputeMarketSettlement(t *testing.T) {
	market := NewMarket()
	providerA := types.AccountIDFromPublicKey([]byte("provider-a"))
	providerB := types.AccountIDFromPublicKey([]byte("provider-b"))
	owner := types.AccountIDFromPublicKey([]byte("job-owner"))
	resources := Resources{CPUCores: 4, MemoryMiB: 4096, GPUCount: 1, GPUMemoryMiB: 8192, StorageMiB: 1024, BandwidthMbps: 100, Capabilities: []string{"cuda"}}
	offerA := Offer{Provider: providerA, Resources: resources, PricePerUnit: 3, Collateral: 10, Verification: []VerificationMode{VerificationReplicated}, ValidUntilHeight: 100}
	offerB := Offer{Provider: providerB, Resources: resources, PricePerUnit: 4, Collateral: 10, Verification: []VerificationMode{VerificationReplicated}, ValidUntilHeight: 100}
	offerAID, err := market.PublishOffer(offerA, 1)
	if err != nil {
		t.Fatal(err)
	}
	offerBID, err := market.PublishOffer(offerB, 1)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{
		Owner: owner, WorkloadHash: types.HashBytes("workload", []byte("render")), InputRoot: types.HashBytes("input", []byte("scene")),
		Resources: resources, MaxPrice: 10, CollateralRequired: 5, Verification: VerificationReplicated,
		DeadlineHeight: 50, Replicas: 2,
	}
	jobID, err := market.PostJob(job, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.Assign(jobID, offerAID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := market.Assign(jobID, offerBID, 3); err != nil {
		t.Fatal(err)
	}
	root := types.HashBytes("result", []byte("same-output"))
	if err := market.SubmitResult(Result{JobID: jobID, Provider: providerA, ResultRoot: root, CompletedHeight: 10}); err != nil {
		t.Fatal(err)
	}
	if err := market.SubmitResult(Result{JobID: jobID, Provider: providerB, ResultRoot: root, CompletedHeight: 11}); err != nil {
		t.Fatal(err)
	}
	settlement, err := market.Finalize(jobID, VerificationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if settlement.ResultRoot != root || settlement.Payments[providerA] != 3 || settlement.Payments[providerB] != 4 || settlement.Refund != 3 {
		t.Fatalf("unexpected settlement: %+v", settlement)
	}
}

func TestComputeMarketRequiresVerificationEvidence(t *testing.T) {
	market := NewMarket()
	provider := types.AccountIDFromPublicKey([]byte("provider"))
	owner := types.AccountIDFromPublicKey([]byte("owner"))
	resources := Resources{CPUCores: 2, MemoryMiB: 2048}
	offer := Offer{Provider: provider, Resources: resources, PricePerUnit: 2, Collateral: 5, Verification: []VerificationMode{VerificationZeroKnowledge}, ValidUntilHeight: 20}
	offerID, err := market.PublishOffer(offer, 1)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{Owner: owner, WorkloadHash: types.HashBytes("workload", []byte("zk")), InputRoot: types.HashBytes("input", []byte("x")), Resources: resources, MaxPrice: 3, CollateralRequired: 1, Verification: VerificationZeroKnowledge, DeadlineHeight: 15}
	jobID, err := market.PostJob(job, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.Assign(jobID, offerID, 3); err != nil {
		t.Fatal(err)
	}
	result := Result{JobID: jobID, Provider: provider, ResultRoot: types.HashBytes("result", []byte("r")), ProofHash: types.HashBytes("proof", []byte("p")), CompletedHeight: 5}
	if err := market.SubmitResult(result); err != nil {
		t.Fatal(err)
	}
	if _, err := market.Finalize(jobID, VerificationEvidence{}); err != ErrMarketVerification {
		t.Fatalf("expected proof verification gate, got %v", err)
	}
	if _, err := market.Finalize(jobID, VerificationEvidence{ProofVerified: true}); err != nil {
		t.Fatal(err)
	}
}
