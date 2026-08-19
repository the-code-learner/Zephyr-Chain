package compute

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestWorkSpecRoundTripAndVerifiedSettlementObservation(t *testing.T) {
	var workload, benchmark, resultRoot types.Hash
	var jobID types.JobID
	var provider types.AccountID
	workload[0] = 1
	benchmark[0] = 2
	resultRoot[0] = 3
	jobID[0] = 4
	provider[0] = 5

	spec := WorkSpec{
		Version: WorkSpecVersion,
		Class: WorkTensorAI,
		Units: 250,
		WorkloadHash: workload,
		BenchmarkHash: benchmark,
		Vector: WorkVector{TensorUnits: 250, VRAMByteSeconds: 8 << 30},
	}
	raw, err := spec.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseWorkSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != spec {
		t.Fatalf("work spec round trip mismatch: %#v != %#v", parsed, spec)
	}
	registry, err := NewWorkRegistry([]WorkSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	record := OnChainJob{
		ID: jobID,
		Job: Job{WorkloadHash: workload, Verification: VerificationReplicated},
		Status: JobSettled,
	}
	settlement := OnChainSettlement{Settlement: Settlement{
		JobID: jobID,
		ResultRoot: resultRoot,
		Payments: map[types.AccountID]uint64{provider: 5000},
	}}
	observation, err := ObserveVerifiedWork(record, settlement, registry)
	if err != nil {
		t.Fatal(err)
	}
	if observation.PaidZPH != 5000 || observation.Units != 250 || observation.Class != WorkTensorAI {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func TestWorkRegistryRejectsConflictingDefinition(t *testing.T) {
	var workload, benchmarkA, benchmarkB types.Hash
	workload[0] = 1
	benchmarkA[0] = 2
	benchmarkB[0] = 3
	base := WorkSpec{
		Version: WorkSpecVersion,
		Class: WorkCPUGeneral,
		Units: 1,
		WorkloadHash: workload,
		BenchmarkHash: benchmarkA,
		Vector: WorkVector{CPUUnits: 1},
	}
	registry, err := NewWorkRegistry([]WorkSpec{base})
	if err != nil {
		t.Fatal(err)
	}
	conflicting := base
	conflicting.BenchmarkHash = benchmarkB
	if err := registry.Register(conflicting); err != ErrInvalidWorkRegistry {
		t.Fatalf("expected conflicting registry definition rejection, got %v", err)
	}
}
