package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCommitPreflightRejectsStaleLaterShardBeforeAnyApply(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("preflight-stale")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	state0 := worldstate.NewMemory()
	state1 := worldstate.NewMemory()
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: state0, 1: state1}, 1)
	if err != nil {
		t.Fatal(err)
	}

	old0 := systemObjectForShard(0, 1, "old-0")
	old1 := systemObjectForShard(1, 1, "old-1")
	if _, err := state0.Apply(nil, []object.Object{old0}); err != nil {
		t.Fatal(err)
	}
	if _, err := state1.Apply(nil, []object.Object{old1}); err != nil {
		t.Fatal(err)
	}
	new0 := systemObjectForShard(0, 2, "new-0")
	new1 := systemObjectForShard(1, 2, "new-1")
	deltas := map[uint32]shardDelta{
		0: {Consumed: []types.ObjectID{old0.ID}, Created: []object.Object{new0}},
		1: {Consumed: []types.ObjectID{old1.ID}, Created: []object.Object{new1}},
	}
	candidate := manualCandidateForDeltas(t, runtime, deltas)

	root0Before := state0.Root()
	interloper := systemObjectForShard(1, 3, "interloper")
	if _, err := state1.Apply([]types.ObjectID{old1.ID}, []object.Object{interloper}); err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidateExpectError(t, runtime, key, validators, candidate)

	if state0.Root() != root0Before {
		t.Fatal("shard 0 mutated before stale shard 1 was rejected")
	}
	if _, ok := state0.GetObject(old0.ID); !ok {
		t.Fatal("shard 0 consumed object despite failed all-shard preflight")
	}
	if _, ok := state0.GetObject(new0.ID); ok {
		t.Fatal("shard 0 created object despite failed all-shard preflight")
	}
	if runtime.Height != 0 {
		t.Fatalf("failed preflight advanced runtime height to %d", runtime.Height)
	}
}

func TestCommitPreflightRejectsTamperedUntouchedShardRoot(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("preflight-root")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: worldstate.NewMemory(), 1: worldstate.NewMemory()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := runtime.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Commitments[1].StateRoot = types.Hash{99}
	root, err := sharding.CommitmentRoot(candidate.Commitments)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Header.ShardCommitmentRoot = root
	commitSchedulerCandidateExpectError(t, runtime, key, validators, candidate)
	if runtime.Height != 0 {
		t.Fatal("tampered untouched shard commitment finalized")
	}
}

func TestCommitPreflightRejectsTamperedHeaderDataRoot(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("preflight-data")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := runtime.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Header.DataRoot = types.Hash{77}
	commitSchedulerCandidateExpectError(t, runtime, key, validators, candidate)
	if runtime.Height != 0 {
		t.Fatal("tampered global data root finalized")
	}
}

func manualCandidateForDeltas(t *testing.T, runtime *Runtime, deltas map[uint32]shardDelta) Candidate {
	t.Helper()
	commitments := make([]sharding.Commitment, 0, runtime.ShardCount)
	dataLeaves := make([]types.Hash, runtime.ShardCount)
	receipts := make(map[uint32][]sharding.CrossShardReceipt, runtime.ShardCount)
	for shard := uint32(0); shard < runtime.ShardCount; shard++ {
		stateRoot := runtime.States[shard].Root()
		if delta, ok := deltas[shard]; ok {
			simulator := runtime.States[shard].(worldstate.Simulator)
			var err error
			stateRoot, err = simulator.Simulate(delta.Consumed, delta.Created)
			if err != nil {
				t.Fatal(err)
			}
		}
		receiptRoot, err := (sharding.ReceiptBatch{}).Root()
		if err != nil {
			t.Fatal(err)
		}
		dataRoot := merkle.Root(nil)
		commitments = append(commitments, sharding.Commitment{ShardID: shard, StateRoot: stateRoot, ReceiptRoot: receiptRoot, DataRoot: dataRoot})
		dataLeaves[shard] = merkle.Leaf("shard-data-root", dataRoot[:])
	}
	commitmentRoot, err := sharding.CommitmentRoot(commitments)
	if err != nil {
		t.Fatal(err)
	}
	return Candidate{
		Header: sharding.GlobalHeader{
			Version: 2, Network: runtime.Network, Height: 1, ParentHash: runtime.ParentHash,
			ShardCommitmentRoot: commitmentRoot, ValidatorRoot: runtime.ValidatorRoot,
			NextValidatorRoot: runtime.ValidatorRoot, DataRoot: merkle.Root(dataLeaves),
		},
		Commitments: commitments,
		Results:     make(map[uint32][]execution.Result),
		Receipts:    receipts,
		deltas:      deltas,
	}
}

func systemObjectForShard(shard, index uint32, label string) object.Object {
	return object.Object{
		ID:      types.ObjectIDForShard(types.HashBytes("preflight-object", []byte(label)), index, shard),
		Version: 1,
		Kind:    object.KindSystem,
		Data:    []byte(label),
	}
}
