package node

import (
	"reflect"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCandidateShardSchedulePrioritizesActiveAndKeepsStableIndices(t *testing.T) {
	transaction := tx.Transaction{}
	for _, tc := range []struct {
		name    string
		batches map[uint32]ShardBatch
		want    []uint32
	}{
		{name: "all idle", want: []uint32{0, 1, 2, 3}},
		{name: "last shard active", batches: map[uint32]ShardBatch{3: {Transactions: []tx.Transaction{transaction}}}, want: []uint32{3, 0, 1, 2}},
		{name: "two active shards", batches: map[uint32]ShardBatch{1: {Transactions: []tx.Transaction{transaction}}, 3: {Transactions: []tx.Transaction{transaction}}}, want: []uint32{1, 3, 0, 2}},
		{name: "empty batch remains idle", batches: map[uint32]ShardBatch{1: {}}, want: []uint32{0, 1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := candidateShardSchedule(4, tc.batches); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("schedule=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestCandidatePrioritizedSchedulingPreservesCanonicalRootsAndError(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("schedule-priority")))
	token := types.TokenID(types.HashBytes("token", []byte("schedule-priority")))
	validators := types.HashBytes("validators", []byte("schedule-priority"))
	makeRuntime := func(workers int) *Runtime {
		t.Helper()
		states := make(map[uint32]worldstate.Backend, 4)
		for shard := uint32(0); shard < 4; shard++ {
			states[shard] = worldstate.NewMemory()
		}
		r, err := NewRuntime(network, token, validators, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	serial := makeRuntime(1)
	parallel := makeRuntime(4)
	batches := map[uint32]ShardBatch{3: {DataRoot: types.HashBytes("data", []byte("last-shard"))}}
	a, err := serial.BuildCandidate(1, batches)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parallel.BuildCandidate(1, batches)
	if err != nil {
		t.Fatal(err)
	}
	if a.Header != b.Header || len(a.Commitments) != len(b.Commitments) {
		t.Fatal("prioritized parallel candidate changed canonical header")
	}
	for shard := range a.Commitments {
		if a.Commitments[shard] != b.Commitments[shard] {
			t.Fatalf("shard %d commitment changed", shard)
		}
	}
	bad := tx.Transaction{StateRoot: types.HashBytes("state", []byte("bad"))}
	invalid := map[uint32]ShardBatch{1: {Transactions: []tx.Transaction{bad}}, 3: {Transactions: []tx.Transaction{bad}}}
	if _, err := parallel.BuildCandidate(1, invalid); err != ErrCandidateState {
		t.Fatalf("invalid active shards accepted: %v", err)
	}
	if parallel.Height != 0 {
		t.Fatal("invalid candidate advanced finalized height")
	}
}
