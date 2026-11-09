package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

// Map allocation hints must not influence canonical output, sparse result
// exposure, per-shard receipt entries or worker scheduling.
func TestCandidateMapCapacityPreservesSixteenShardCanonicalHeader(t *testing.T) {
	network := types.NetworkID(types.HashBytes("candidate-map-capacity", []byte("network")))
	token := types.TokenID(types.HashBytes("candidate-map-capacity", []byte("token")))
	root := types.HashBytes("candidate-map-capacity", []byte("validators"))
	batches := map[uint32]ShardBatch{
		9: {DataRoot: types.HashBytes("candidate-map-capacity", []byte("shard-9-data"))},
	}
	var reference Candidate
	for _, workers := range []int{1, 4, 16} {
		states := make(map[uint32]worldstate.Backend, 16)
		for i := uint32(0); i < 16; i++ {
			states[i] = worldstate.NewMemory()
		}
		rt, err := NewRuntime(network, token, root, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := rt.BuildCandidate(1, batches)
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		if len(candidate.Commitments) != 16 || len(candidate.Receipts) != 16 ||
			len(candidate.Results) != 0 || len(candidate.deltas) != 0 ||
			candidate.economicObservations == nil {
			t.Fatalf("workers=%d: candidate maps/commitments unexpectedly changed", workers)
		}
		for shard := uint32(0); shard < 16; shard++ {
			if _, exists := candidate.Receipts[shard]; !exists {
				t.Fatalf("workers=%d: missing canonical empty receipt entry at shard %d", workers, shard)
			}
		}
		if reference.Commitments == nil {
			reference = candidate
			continue
		}
		if candidate.Header != reference.Header {
			t.Fatalf("workers=%d: canonical header differs after capacity hint", workers)
		}
		for i, commitment := range candidate.Commitments {
			if commitment != reference.Commitments[i] {
				t.Fatalf("workers=%d shard=%d: commitment differs", workers, i)
			}
		}
	}
}
