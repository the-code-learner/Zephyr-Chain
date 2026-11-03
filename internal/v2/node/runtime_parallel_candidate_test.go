package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestConcurrentShardCandidateMatchesSequentialAndFailsClosed(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("candidate-parallel")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	root := types.HashBytes("validators", []byte("candidate-parallel"))
	newRuntime := func(workers int) *Runtime {
		t.Helper()
		states := make(map[uint32]worldstate.Backend, 4)
		for shard := uint32(0); shard < 4; shard++ {
			states[shard] = worldstate.NewMemory()
		}
		r, err := NewRuntime(network, native, root, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	serial := newRuntime(1)
	parallel := newRuntime(4)
	a, err := serial.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parallel.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Header != b.Header || len(a.Commitments) != 4 || len(b.Commitments) != 4 {
		t.Fatalf("parallel candidate differs from canonical serial result: a=%+v b=%+v", a.Header, b.Header)
	}
	for shard := range a.Commitments {
		if a.Commitments[shard] != b.Commitments[shard] {
			t.Fatalf("shard %d commitment differs", shard)
		}
	}
	bad := map[uint32]ShardBatch{
		0: {Transactions: []tx.Transaction{{ShardID: 0, StateRoot: types.HashBytes("bad", []byte("root"))}}},
		2: {Transactions: []tx.Transaction{{ShardID: 2, StateRoot: types.HashBytes("bad", []byte("root"))}}},
	}
	if _, err := parallel.BuildCandidate(1, bad); err != ErrCandidateState {
		t.Fatalf("parallel malformed candidate not rejected: %v", err)
	}
	if parallel.Height != 0 {
		t.Fatal("invalid candidate mutated finalized height")
	}
}
