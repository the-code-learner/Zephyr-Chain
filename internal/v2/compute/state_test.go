package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestComputeProtocolObjectsRoundTrip(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("compute-owner"))
	provider := types.AccountIDFromPublicKey([]byte("compute-provider"))
	offer := Offer{
		Provider: provider,
		Resources: Resources{CPUCores: 8, MemoryMiB: 16384, GPUCount: 1, GPUMemoryMiB: 24576, StorageMiB: 102400, BandwidthMbps: 1000, Capabilities: []string{"cuda", "render"}},
		PricePerUnit: 25, Collateral: 100, Verification: []VerificationMode{VerificationReplicated, VerificationTEE}, ValidUntilHeight: 500,
	}
	rawOffer, err := offer.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedOffer, err := ParseOffer(rawOffer)
	if err != nil || parsedOffer.Provider != provider || parsedOffer.Resources.GPUCount != 1 {
		t.Fatalf("offer round trip failed: %+v %v", parsedOffer, err)
	}

	job := Job{
		Owner: owner, WorkloadHash: types.HashBytes("workload", []byte("render-scene")), InputRoot: types.HashBytes("input", []byte("scene")),
		Resources: Resources{CPUCores: 4, MemoryMiB: 8192, GPUCount: 1, GPUMemoryMiB: 8192, StorageMiB: 2048, BandwidthMbps: 100, Capabilities: []string{"render"}},
		MaxPrice: 50, CollateralRequired: 50, Verification: VerificationReplicated, DeadlineHeight: 600, Replicas: 2,
	}
	rawJob, err := job.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if parsedJob, err := ParseJob(rawJob); err != nil || parsedJob.Owner != owner || parsedJob.Replicas != 2 {
		t.Fatalf("job round trip failed: %+v %v", parsedJob, err)
	}

	txID := types.HashBytes("tx", []byte("compute-job"))
	jobObject, record, err := NewJobObject(txID, 0, job, 50)
	if err != nil {
		t.Fatal(err)
	}
	if jobObject.Kind != object.KindComputeJob || record.Status != JobPending {
		t.Fatal("unexpected compute job object")
	}
	assignment := Assignment{OfferID: types.HashBytes("offer", []byte("1")), Provider: provider, Price: 25}
	result := Result{JobID: record.ID, Provider: provider, ResultRoot: types.HashBytes("result", []byte("root")), CompletedHeight: 550}
	record.Assignments = []Assignment{assignment}
	record.Results = []Result{result}
	record.Status = JobAwaitingVerification
	rawRecord, err := record.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOnChainJob(rawRecord)
	if err != nil || parsed.ID != record.ID || len(parsed.Assignments) != 1 || len(parsed.Results) != 1 {
		t.Fatalf("on-chain record round trip failed: %+v %v", parsed, err)
	}
}
