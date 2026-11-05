package node

import (
	"runtime"
	"sort"
	"sync"

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
	// Reject receipt entries for shards outside the committed topology.
	// Otherwise these entries are skipped by the in-range verification loop,
	// permitting a candidate to carry uncommitted receipt data.
	for shard := range candidate.Receipts {
		if shard >= r.ShardCount {
			return nil, nil, ErrCandidateState
		}
	}
	// All shard-keyed candidate metadata must belong to the configured
	// topology, even when a field is not needed by the current commit path.
	// Reject malformed candidates before expensive state simulation.
	for shard := range candidate.Results {
		if shard >= r.ShardCount {
			return nil, nil, ErrCandidateState
		}
	}
	for shard := range candidate.economicObservations {
		if shard >= r.ShardCount {
			return nil, nil, ErrCandidateState
		}
	}
	for shard := range candidate.deltas {
		if shard >= r.ShardCount {
			return nil, nil, ErrCandidateState
		}
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

	// Shard backends are independent. Simulate and verify each shard
	// concurrently, but join every worker before deciding whether any state
	// may be mutated. Error selection stays deterministic by shard index.
	dataLeaves := make([]types.Hash, r.ShardCount)
	shardErrors := make([]error, r.ShardCount)
	workers := r.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > int(r.ShardCount) {
		workers = int(r.ShardCount)
	}
	if workers < 1 {
		workers = 1
	}
	verifyShard := func(shard uint32) error {
		commitment, ok := commitments[shard]
		if !ok {
			return ErrCandidateState
		}
		stateRoot := r.States[shard].Root()
		if delta, changed := candidate.deltas[shard]; changed {
			simulator, ok := r.States[shard].(worldstate.Simulator)
			if !ok {
				return ErrStateSimulation
			}
			var err error
			stateRoot, err = simulator.Simulate(delta.Consumed, delta.Created)
			if err != nil {
				return err
			}
		}
		if stateRoot != commitment.StateRoot {
			return ErrCandidateState
		}
		receiptRoot, err := (sharding.ReceiptBatch{Receipts: candidate.Receipts[shard]}).Root()
		if err != nil || receiptRoot != commitment.ReceiptRoot {
			return ErrCandidateState
		}
		dataLeaves[shard] = merkle.Leaf("shard-data-root", commitment.DataRoot[:])
		return nil
	}
	if workers == 1 {
		for shard := uint32(0); shard < r.ShardCount; shard++ {
			if err := verifyShard(shard); err != nil {
				return nil, nil, err
			}
		}
	} else {
		// Bound dispatch buffering to the number of workers. Prioritize
		// expensive state simulations, but keep all verification results
		// indexed by shard so errors retain deterministic ordering.
		jobs := make(chan uint32, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for shard := range jobs {
					shardErrors[shard] = verifyShard(shard)
				}
			}()
		}
		for _, shard := range preflightShardSchedule(r.ShardCount, candidate.deltas) {
			jobs <- shard
		}
		close(jobs)
		wg.Wait()
		for _, err := range shardErrors {
			if err != nil {
				return nil, nil, err
			}
		}
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

// preflightShardSchedule dispatches state-changing shards before unchanged
// shards. The dispatch order does not change canonical result assembly, error
// selection, or the subsequent ordered application of state.
func preflightShardSchedule(shardCount uint32, deltas map[uint32]shardDelta) []uint32 {
	order := make([]uint32, 0, shardCount)
	for shard := uint32(0); shard < shardCount; shard++ {
		if _, changed := deltas[shard]; changed {
			order = append(order, shard)
		}
	}
	for shard := uint32(0); shard < shardCount; shard++ {
		if _, changed := deltas[shard]; !changed {
			order = append(order, shard)
		}
	}
	return order
}
