package node

import (
	"reflect"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestPreflightShardScheduleChangedFirstAndCanonicalIndices(t *testing.T) {
	cases := []struct {
		name   string
		deltas map[uint32]shardDelta
		want   []uint32
	}{
		{name: "unchanged", want: []uint32{0, 1, 2, 3}},
		{name: "last changed", deltas: map[uint32]shardDelta{3: {}}, want: []uint32{3, 0, 1, 2}},
		{name: "multiple changed", deltas: map[uint32]shardDelta{3: {}, 1: {}}, want: []uint32{1, 3, 0, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := preflightShardSchedule(4, tc.deltas); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("schedule=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestParallelPreflightChangedShardPriorityMatchesSerialRejection(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("preflight-priority")))
	native := types.TokenID(types.HashBytes("token", []byte("preflight-priority")))
	validators := types.HashBytes("validators", []byte("preflight-priority"))
	for _, workers := range []int{1, 4} {
		states := make(map[uint32]worldstate.Backend, 4)
		for shard := uint32(0); shard < 4; shard++ {
			states[shard] = worldstate.NewMemory()
		}
		r, err := NewRuntime(network, native, validators, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := r.BuildCandidate(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.preflightCandidateState(candidate); err != nil {
			t.Fatalf("workers=%d: valid candidate failed preflight: %v", workers, err)
		}
		candidate.deltas[3] = shardDelta{}
		if _, _, err := r.preflightCandidateState(candidate); err != nil {
			t.Fatalf("workers=%d: unchanged empty delta rejected: %v", workers, err)
		}
		candidate.Commitments[3].StateRoot = types.HashBytes("state", []byte("tampered"))
		root, err := sharding.CommitmentRoot(candidate.Commitments)
		if err != nil {
			t.Fatal(err)
		}
		candidate.Header.ShardCommitmentRoot = root
		if _, _, err := r.preflightCandidateState(candidate); err != ErrCandidateState {
			t.Fatalf("workers=%d: tampered commitment accepted: %v", workers, err)
		}
		if r.Height != 0 {
			t.Fatalf("workers=%d: preflight advanced chain height", workers)
		}
	}
}
