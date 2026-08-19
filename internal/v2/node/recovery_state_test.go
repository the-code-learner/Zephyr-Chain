package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

type uncertainBackend struct {
	*worldstate.Memory
	failAfterApply bool
}

func (b *uncertainBackend) Apply(consumed []types.ObjectID, created []object.Object) (types.Hash, error) {
	root, err := b.Memory.Apply(consumed, created)
	if err != nil {
		return root, err
	}
	if b.failAfterApply {
		return root, errors.New("simulated durable write acknowledgement failure")
	}
	return root, nil
}

func TestRuntimeFailStopsAfterUncertainStateApply(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("fail-stop")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	backend := &uncertainBackend{Memory: worldstate.NewMemory()}
	oldObject := systemObjectForShard(0, 1, "fail-stop-old")
	if _, err := backend.Memory.Apply(nil, []object.Object{oldObject}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: backend}, 1)
	if err != nil {
		t.Fatal(err)
	}
	newObject := systemObjectForShard(0, 2, "fail-stop-new")
	candidate := manualCandidateForDeltas(t, runtime, map[uint32]shardDelta{
		0: {Consumed: []types.ObjectID{oldObject.ID}, Created: []object.Object{newObject}},
	})
	backend.failAfterApply = true
	commitSchedulerCandidateExpectError(t, runtime, key, validators, candidate)

	if !runtime.RecoveryRequired() {
		t.Fatal("runtime did not enter recovery-required state after uncertain apply")
	}
	if runtime.Height != 0 {
		t.Fatalf("uncertain apply advanced consensus height to %d", runtime.Height)
	}
	if _, ok := backend.GetObject(newObject.ID); !ok {
		t.Fatal("test backend did not simulate a post-mutation acknowledgement failure")
	}
	if _, err := runtime.BuildCandidate(1, nil); !errors.Is(err, ErrRuntimeRecoveryRequired) {
		t.Fatalf("runtime continued building after uncertain state apply: %v", err)
	}
	if _, err := runtime.Commit(candidate, v2consensus.Certificate{}, validators); !errors.Is(err, ErrRuntimeRecoveryRequired) {
		t.Fatalf("runtime continued committing after uncertain state apply: %v", err)
	}
}
