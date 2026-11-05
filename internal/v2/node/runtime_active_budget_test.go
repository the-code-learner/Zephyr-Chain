package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCandidateWorkerBudgetFollowsActiveShards(t *testing.T) {
	transaction := tx.Transaction{}
	cases := []struct {
		name string
		batches map[uint32]ShardBatch
		shardWorkers, transactionWorkers int
	}{
		{name: "all idle", shardWorkers: 4, transactionWorkers: 2},
		{name: "one active transaction shard", batches: map[uint32]ShardBatch{3: {Transactions: []tx.Transaction{transaction}}}, shardWorkers: 1, transactionWorkers: 8},
		{name: "two active shards", batches: map[uint32]ShardBatch{0: {Transactions: []tx.Transaction{transaction}}, 3: {Transactions: []tx.Transaction{transaction}}}, shardWorkers: 2, transactionWorkers: 4},
		{name: "empty sparse batch is idle", batches: map[uint32]ShardBatch{1: {}}, shardWorkers: 4, transactionWorkers: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outer, inner := candidateWorkerAllocationForBatches(8, 4, tc.batches)
			if outer != tc.shardWorkers || inner != tc.transactionWorkers || outer*inner > 8 {
				t.Fatalf("workers=(%d,%d), expected=(%d,%d)", outer, inner, tc.shardWorkers, tc.transactionWorkers)
			}
		})
	}
}

func TestActivityAwareBudgetDoesNotChangeIdleShardCommitments(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("activity-budget")))
	token := types.TokenID(types.HashBytes("token", []byte("activity-budget")))
	validators := types.HashBytes("validators", []byte("activity-budget"))
	var reference Candidate
	for _, workers := range []int{1, 4, 8} {
		states := make(map[uint32]worldstate.Backend, 4)
		for shard := uint32(0); shard < 4; shard++ {
			states[shard] = worldstate.NewMemory()
		}
		r, err := NewRuntime(network, token, validators, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := r.BuildCandidate(1, map[uint32]ShardBatch{3: {}})
		if err != nil {
			t.Fatal(err)
		}
		if reference.Commitments == nil {
			reference = candidate
			continue
		}
		if candidate.Header != reference.Header {
			t.Fatalf("workers=%d: header changed", workers)
		}
		for i, commitment := range candidate.Commitments {
			if commitment != reference.Commitments[i] {
				t.Fatalf("workers=%d: shard %d commitment changed", workers, i)
			}
		}
	}
}
