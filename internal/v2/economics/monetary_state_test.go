package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestShadowMonetaryEpochStateRoundTripAndNoLiveMint(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("economics-state")))
	aggregate := EpochAggregate{
		Epoch: 1, ShardCount: 1,
		ChargedFees: 100, BurnedFees: 100, FinalizedOperations: 1_000,
		ResourceUsed: 50, ResourceCapacity: 100, ResourceUtilizationBps: 5_000,
		CirculatingNativeSupply: 900_000_000, AgeWeightedVelocityBps: 5_000,
		EscrowBackedComputeDemand: 2_000, VerifiedComputeSupply: 1_000,
		ComputeBacklog: 500, ComputeFulfilled: 1_000, ComputeExpired: 500, ComputeUtilizationBps: 10_000,
	}
	scarcity, err := BuildComputeScarcity(1, aggregate.ComputeMarketMetrics(1_000, true), DefaultComputeScarcityConfig())
	if err != nil {
		t.Fatal(err)
	}
	monetary := DefaultShadowPolicy()
	feedbackPolicy := DefaultComputeFeedbackPolicy(ComputeFeedbackMonetaryBand)
	state, base, feedback, err := BuildShadowMonetaryEpochState(
		network, nil, aggregate, 1_000_000_000, 450_000_000, 100_000_000,
		7_500_000_000, 1_000, true, scarcity, monetary, feedbackPolicy, 10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Shadow || state.TotalSupply != 1_000_000_000 || state.ShadowGrossMintTarget == 0 {
		t.Fatalf("unexpected shadow state: %#v", state)
	}
	if feedback.SuggestedGrossMint != state.ShadowGrossMintTarget || base.ProjectedNetChange == 0 {
		t.Fatalf("shadow decisions were not committed correctly: %#v %#v", base, feedback)
	}
	raw, err := state.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseMonetaryEpochState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != state {
		t.Fatalf("monetary state round trip mismatch: %#v != %#v", parsed, state)
	}
	obj, err := state.Object()
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != MonetaryStateObjectID(network) || obj.Version != state.Epoch {
		t.Fatalf("unexpected monetary system object: %#v", obj)
	}
}

func TestShadowMonetaryEpochStateChainsPreviousEpoch(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("economics-chain")))
	policy := DefaultShadowPolicy()
	feedbackPolicy := DefaultComputeFeedbackPolicy(ComputeFeedbackObserveOnly)
	makeAggregate := func(epoch uint64) EpochAggregate {
		return EpochAggregate{
			Epoch: epoch, ShardCount: 1, ChargedFees: 10, BurnedFees: 10,
			FinalizedOperations: 100, ResourceUsed: 50, ResourceCapacity: 100, ResourceUtilizationBps: 5_000,
			CirculatingNativeSupply: 900_000_000, AgeWeightedVelocityBps: 5_000,
			EscrowBackedComputeDemand: 1_000, VerifiedComputeSupply: 1_000,
			ComputeFulfilled: 700, ComputeBacklog: 300, ComputeUtilizationBps: 7_000,
		}
	}
	firstAggregate := makeAggregate(1)
	firstScarcity, err := BuildComputeScarcity(1, firstAggregate.ComputeMarketMetrics(0, false), DefaultComputeScarcityConfig())
	if err != nil {
		t.Fatal(err)
	}
	first, _, _, err := BuildShadowMonetaryEpochState(network, nil, firstAggregate, 1_000_000_000, 450_000_000, 100_000_000, 0, 0, false, firstScarcity, policy, feedbackPolicy, 10)
	if err != nil {
		t.Fatal(err)
	}
	secondAggregate := makeAggregate(2)
	secondScarcity, err := BuildComputeScarcity(2, secondAggregate.ComputeMarketMetrics(0, false), DefaultComputeScarcityConfig())
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := BuildShadowMonetaryEpochState(network, &first, secondAggregate, 1_000_000_000, 450_000_000, 100_000_000, 0, 0, false, secondScarcity, policy, feedbackPolicy, 10)
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if second.PreviousStateHash != firstHash {
		t.Fatal("monetary epoch state did not bind previous state")
	}

	thirdAggregate := makeAggregate(4)
	thirdScarcity, err := BuildComputeScarcity(4, thirdAggregate.ComputeMarketMetrics(0, false), DefaultComputeScarcityConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildShadowMonetaryEpochState(network, &second, thirdAggregate, 1_000_000_000, 450_000_000, 100_000_000, 0, 0, false, thirdScarcity, policy, feedbackPolicy, 10); err != ErrMonetaryState {
		t.Fatalf("skipped epoch accepted: %v", err)
	}
}
