package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestReplicatedJobRequiresMultipleReplicas(t *testing.T) {
	job := Job{
		Owner:        types.AccountIDFromPublicKey([]byte("owner")),
		WorkloadHash: types.HashBytes("workload", []byte("container")),
		InputRoot:    types.HashBytes("input", []byte("dataset")),
		Resources:    Resources{CPUCores: 4, MemoryMiB: 8192},
		MaxPrice:     100, Verification: VerificationReplicated,
		DeadlineHeight: 100, Replicas: 1,
	}
	if err := job.Validate(); err == nil {
		t.Fatal("replicated job accepted one replica")
	}
	job.Replicas = 3
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
}
