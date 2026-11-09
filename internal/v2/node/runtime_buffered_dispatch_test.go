package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

// A buffered dispatch queue must not change canonical ordering or quietly
// omit idle shard commitments when active shards are scheduled first.
func TestBufferedShardDispatchPreservesThirtyTwoShardCommitments(t *testing.T) {
	network := types.NetworkID(types.HashBytes("buffered-dispatch", []byte("network")))
	token := types.TokenID(types.HashBytes("buffered-dispatch", []byte("token")))
	validatorRoot := types.HashBytes("buffered-dispatch", []byte("validators"))
	batches := map[uint32]ShardBatch{
		2:  {DataRoot: types.HashBytes("buffered-dispatch", []byte("shard-2"))},
		17: {DataRoot: types.HashBytes("buffered-dispatch", []byte("shard-17"))},
		30: {DataRoot: types.HashBytes("buffered-dispatch", []byte("shard-30"))},
	}
	var baseline Candidate
	for _, workers := range []int{1, 2, 4, 8, 32} {
		states := make(map[uint32]worldstate.Backend, 32)
		for shard := uint32(0); shard < 32; shard++ {
			states[shard] = worldstate.NewMemory()
		}
		rt, err := NewRuntime(network, token, validatorRoot, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := rt.BuildCandidate(1, batches)
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		if len(candidate.Commitments) != 32 || len(candidate.Receipts) != 32 {
			t.Fatalf("workers=%d: candidate silently dropped shard", workers)
		}
		for shard := uint32(0); shard < 32; shard++ {
			if _, ok := candidate.Receipts[shard]; !ok {
				t.Fatalf("workers=%d: missing empty receipt entry for shard=%d", workers, shard)
			}
		}
		if baseline.Commitments == nil {
			baseline = candidate
			continue
		}
		if candidate.Header != baseline.Header {
			t.Fatalf("workers=%d: parallel dispatch changed canonical global header", workers)
		}
		for shard, commitment := range candidate.Commitments {
			if commitment != baseline.Commitments[shard] {
				t.Fatalf("workers=%d shard=%d: parallel dispatch changed canonical commitment", workers, shard)
			}
		}
	}
}
