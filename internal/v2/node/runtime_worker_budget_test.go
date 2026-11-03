package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCandidateWorkerAllocationBounded(t *testing.T) {
	for _, tc := range []struct {
		budget, shards, outer, inner int
	}{
		{1, 4, 1, 1},
		{4, 1, 1, 4},
		{4, 4, 4, 1},
		{8, 3, 3, 2},
		{16, 4, 4, 4},
		{2, 8, 2, 1},
	} {
		outer, inner := candidateWorkerAllocation(tc.budget, uint32(tc.shards))
		if outer != tc.outer || inner != tc.inner || outer*inner > tc.budget {
			t.Fatalf("budget=%d shards=%d: got (%d,%d), want (%d,%d)", tc.budget, tc.shards, outer, inner, tc.outer, tc.inner)
		}
	}
	outer, inner := candidateWorkerAllocation(0, 4)
	if outer < 1 || inner < 1 {
		t.Fatalf("default worker budget invalid: %d %d", outer, inner)
	}
}

func TestCandidateNestedWorkerBudgetPreservesCanonicalCommitments(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("nested-worker-budget")))
	native := types.TokenID(types.HashBytes("token", []byte("nested-worker-budget")))
	validatorRoot := types.HashBytes("validators", []byte("nested-worker-budget"))
	var reference Candidate
	for _, workers := range []int{1, 2, 4, 8} {
		states := make(map[uint32]worldstate.Backend, 4)
		for shard := uint32(0); shard < 4; shard++ {
			states[shard] = worldstate.NewMemory()
		}
		r, err := NewRuntime(network, native, validatorRoot, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := r.BuildCandidate(1, nil)
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		if reference.Commitments == nil {
			reference = candidate
			continue
		}
		if candidate.Header != reference.Header || len(candidate.Commitments) != len(reference.Commitments) {
			t.Fatalf("workers=%d: canonical header changed", workers)
		}
		for i := range candidate.Commitments {
			if candidate.Commitments[i] != reference.Commitments[i] {
				t.Fatalf("workers=%d shard=%d: commitment changed", workers, i)
			}
		}
	}
}
