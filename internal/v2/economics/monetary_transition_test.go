package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestShadowMonetaryTransitionCanBeFinalizedThroughStateRoot(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("monetary-transition")))
	policy := DefaultShadowPolicy()
	feedback := DefaultComputeFeedbackPolicy(ComputeFeedbackObserveOnly)
	makeEpoch := func(epoch uint64, previous *MonetaryEpochState) MonetaryEpochState {
		aggregate := EpochAggregate{
			Epoch: epoch, ShardCount: 1, ChargedFees: 10, BurnedFees: 10,
			FinalizedOperations: 100, ResourceUsed: 50, ResourceCapacity: 100, ResourceUtilizationBps: 5_000,
			CirculatingNativeSupply: 900_000_000, AgeWeightedVelocityBps: 4_000,
			EscrowBackedComputeDemand: 1_000, VerifiedComputeSupply: 1_000, ComputeFulfilled: 700, ComputeUtilizationBps: 7_000,
		}
		scarcity, err := BuildComputeScarcity(epoch, aggregate.ComputeMarketMetrics(0, false), DefaultComputeScarcityConfig())
		if err != nil {
			t.Fatal(err)
		}
		state, _, _, err := BuildShadowMonetaryEpochState(network, previous, aggregate, 1_000_000_000, 450_000_000, 100_000_000, 0, 0, false, scarcity, policy, feedback, 10)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}

	store := worldstate.NewMemory()
	first := makeEpoch(1, nil)
	consumed, created, err := ShadowMonetaryTransition(nil, first)
	if err != nil {
		t.Fatal(err)
	}
	root1, err := store.Apply(consumed, created)
	if err != nil {
		t.Fatal(err)
	}
	firstObject, proof, ok := store.Proof(MonetaryStateObjectID(network))
	if !ok || firstObject.Version != 1 {
		t.Fatal("first monetary state not committed")
	}
	if len(proof.Siblings) == 0 && root1 == (types.Hash{}) {
		t.Fatal("monetary state did not affect authenticated root")
	}

	second := makeEpoch(2, &first)
	consumed, created, err = ShadowMonetaryTransition(&firstObject, second)
	if err != nil {
		t.Fatal(err)
	}
	root2, err := store.Apply(consumed, created)
	if err != nil {
		t.Fatal(err)
	}
	if root2 == root1 {
		t.Fatal("epoch transition did not change state root")
	}
	secondObject, _, ok := store.Proof(MonetaryStateObjectID(network))
	if !ok || secondObject.Version != 2 {
		t.Fatal("second monetary state not committed")
	}
	parsed, err := ParseMonetaryEpochState(secondObject.Data)
	if err != nil || parsed.PreviousStateHash != second.PreviousStateHash {
		t.Fatalf("committed monetary state cannot be verified: %#v %v", parsed, err)
	}
}
