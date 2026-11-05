package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCommitPreflightRejectsOutOfRangeCandidateMetadata(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("candidate-metadata-bounds")))
	native := types.TokenID(types.HashBytes("token", []byte("candidate-metadata-bounds")))
	validators := types.HashBytes("validators", []byte("candidate-metadata-bounds"))
	for _, workers := range []int{1, 4} {
		r, err := NewRuntime(network, native, validators, map[uint32]worldstate.Backend{
			0: worldstate.NewMemory(),
			1: worldstate.NewMemory(),
		}, workers)
		if err != nil {
			t.Fatal(err)
		}
		valid, err := r.BuildCandidate(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.preflightCandidateState(valid); err != nil {
			t.Fatalf("workers=%d: valid candidate rejected: %v", workers, err)
		}
		for _, field := range []string{"results", "economics", "deltas"} {
			candidate := valid
			switch field {
			case "results":
				candidate.Results = map[uint32][]execution.Result{2: {}}
			case "economics":
				candidate.economicObservations = map[uint32]economics.FinalizedShardObservation{2: {}}
			case "deltas":
				candidate.deltas = map[uint32]shardDelta{2: {}}
			}
			if _, _, err := r.preflightCandidateState(candidate); err != ErrCandidateState {
				t.Errorf("workers=%d field=%s: expected ErrCandidateState, got %v", workers, field, err)
			}
			if r.Height != 0 {
				t.Fatal("rejected candidate advanced finalized height")
			}
		}
	}
}
