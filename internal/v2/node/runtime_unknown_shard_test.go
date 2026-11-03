package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestV2BuildCandidateRejectsUnknownShardBatches(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("unknown-shard-batch")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validatorRoot := types.HashBytes("validator-root", []byte("unknown-shard-batch"))
	for _, workers := range []int{1, 4} {
		states := map[uint32]worldstate.Backend{
			0: worldstate.NewMemory(),
			1: worldstate.NewMemory(),
		}
		r, err := NewRuntime(network, native, validatorRoot, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		bad := map[uint32]ShardBatch{2: {}}
		if _, err := r.BuildCandidate(1, bad); err != ErrCandidateState {
			t.Fatalf("workers=%d: accepted out-of-range batch: %v", workers, err)
		}
		if r.Height != 0 || r.recoveryRequired {
			t.Fatalf("workers=%d: rejected candidate changed committed state", workers)
		}
		ok, err := r.BuildCandidate(1, map[uint32]ShardBatch{1: {}})
		if err != nil || len(ok.Commitments) != 2 {
			t.Fatalf("workers=%d: legitimate partial shard batch failed: %v", workers, err)
		}
	}
}
