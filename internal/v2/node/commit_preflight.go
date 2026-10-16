package node

import (
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

// preflightCandidateState reconstructs every shard commitment against the
// currently committed state before Runtime.Commit mutates any backend. This
// catches stale/missing objects, duplicate/conflicting outputs and tampered
// commitment/receipt/data roots deterministically across all shards.
//
// It does not make independent durable backends transactionally atomic against
// a later storage I/O failure; that requires the global commit coordinator.
func (r *Runtime) preflightCandidateState(candidate Candidate) (map[uint32]sharding.Commitment, []int, error) {
	if r == nil || len(candidate.Commitments) != int(r.ShardCount) {
		return nil, nil, ErrCandidateState
	}
	commitmentRoot, err := sharding.CommitmentRoot(candidate.Commitments)
	if err != nil || commitmentRoot != candidate.Header.ShardCommitmentRoot {
		return nil, nil, ErrCandidateState
	}

	commitments := make(map[uint32]sharding.Commitment, r.ShardCount)
	for _, commitment := range candidate.Commitments {
		if commitment.ShardID >= r.ShardCount {
			return nil, nil, ErrCandidateState
		}
		if _, duplicate := commitments[commitment.ShardID]; duplicate {
			return nil, nil, ErrCandidateState
		}
		commitments[commitment.ShardID] = commitment
	}

	dataLeaves := make([]types.Hash, r.ShardCount)
	for shard := uint32(0); shard < r.ShardCount; shard++ {
		commitment, ok := commitments[shard]
		if !ok {
			return nil, nil, ErrCandidateState
		}
		stateRoot := r.States[shard].Root()
		if delta, changed := candidate.deltas[shard]; changed {
			simulator, ok := r.States[shard].(worldstate.Simulator)
			if !ok {
				return nil, nil, ErrStateSimulation
			}
			stateRoot, err = simulator.Simulate(delta.Consumed, delta.Created)
			if err != nil {
				return nil, nil, err
			}
		}
		if stateRoot != commitment.StateRoot {
			return nil, nil, ErrCandidateState
		}

		receiptRoot, err := (sharding.ReceiptBatch{Receipts: candidate.Receipts[shard]}).Root()
		if err != nil || receiptRoot != commitment.ReceiptRoot {
			return nil, nil, ErrCandidateState
		}
		dataLeaves[shard] = merkle.Leaf("shard-data-root", commitment.DataRoot[:])
	}
	if merkle.Root(dataLeaves) != candidate.Header.DataRoot {
		return nil, nil, ErrCandidateState
	}

	shards := make([]int, 0, len(candidate.deltas))
	for shard := range candidate.deltas {
		if shard >= r.ShardCount {
			return nil, nil, ErrCandidateState
		}
		shards = append(shards, int(shard))
	}
	sort.Ints(shards)
	return commitments, shards, nil
}
