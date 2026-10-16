package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func testShadowEpochConfig() ShadowEpochEngineConfig {
	index := ComputeIndexConfig{MinSamplesPerClass: 1, MinCoverageBps: 10_000, EWMABps: 10_000}
	index.WeightsBps[compute.WorkCPUGeneral] = 10_000
	return ShadowEpochEngineConfig{
		ComputeIndex:    index,
		ComputeScarcity: DefaultComputeScarcityConfig(),
		Monetary:        DefaultShadowPolicy(),
		ComputeFeedback: DefaultComputeFeedbackPolicy(ComputeFeedbackRewardRouting),
	}
}

func testEpochMetric(epoch uint64, opening, demand, fulfilled, closing uint64) ShardEpochMetrics {
	return ShardEpochMetrics{
		Version: EpochMetricsVersion, Epoch: epoch, ShardID: 0,
		ChargedFees: 10, BurnedFees: 10,
		FinalizedOperations: 10_000, ResourceUsed: 50, ResourceCapacity: 100,
		CirculatingNativeSupply: 900, AgeWeightedVelocityBps: 5_000,
		EscrowBackedComputeDemand: demand, VerifiedComputeSupply: 1_000,
		ComputeSupplyReliable: true, OpeningComputeBacklog: opening,
		ComputeFulfilled: fulfilled, ComputeBacklog: closing,
	}
}

func testVerifiedCPUWork(jobByte byte, paid uint64) compute.VerifiedWork {
	return compute.VerifiedWork{
		JobID: types.JobID{jobByte}, Class: compute.WorkCPUGeneral, Units: 100,
		PaidZPH: paid, Verification: compute.VerificationReplicated, ResultRoot: types.Hash{jobByte + 1},
	}
}

func TestShadowEpochEnginePreviewDoesNotAdvanceUntilAccepted(t *testing.T) {
	engine, err := NewShadowEpochEngine(types.NetworkID{1}, testShadowEpochConfig())
	if err != nil {
		t.Fatal(err)
	}
	balances := MonetaryBalanceSnapshot{TotalSupply: 1_000, StakedSupply: 400, ProtocolReserve: 100, BaseFee: 1}
	preview, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(1, 0, 1_000, 500, 500)},
		[]compute.VerifiedWork{testVerifiedCPUWork(1, 200)},
		balances,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engine.PreviousState(); ok {
		t.Fatal("preview advanced controller history")
	}
	if !preview.State.Shadow || preview.State.Epoch != 1 || len(preview.Consumed) != 0 || len(preview.Created) != 1 {
		t.Fatalf("unexpected first preview: %#v", preview)
	}
	if !preview.ComputeIndex.Reliable || !preview.ComputeScarcity.Reliable {
		t.Fatalf("expected reliable test economics: index=%#v scarcity=%#v", preview.ComputeIndex, preview.ComputeScarcity)
	}
	if err := engine.Accept(preview); err != nil {
		t.Fatal(err)
	}
	state, ok := engine.PreviousState()
	if !ok || state.Epoch != 1 {
		t.Fatalf("accepted state missing: %#v", state)
	}
}

func TestShadowEpochEngineChainsSecondEpochObject(t *testing.T) {
	engine, err := NewShadowEpochEngine(types.NetworkID{1}, testShadowEpochConfig())
	if err != nil {
		t.Fatal(err)
	}
	balances := MonetaryBalanceSnapshot{TotalSupply: 1_000, StakedSupply: 400, ProtocolReserve: 100, BaseFee: 1}
	first, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(1, 0, 1_000, 500, 500)},
		[]compute.VerifiedWork{testVerifiedCPUWork(1, 200)},
		balances,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Accept(first); err != nil {
		t.Fatal(err)
	}
	second, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(2, 500, 0, 500, 0)},
		[]compute.VerifiedWork{testVerifiedCPUWork(3, 300)},
		balances,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Consumed) != 1 || len(second.Created) != 1 || second.State.PreviousStateHash == (types.Hash{}) {
		t.Fatalf("second epoch did not replace and chain monetary object: %#v", second)
	}
	if err := engine.Accept(second); err != nil {
		t.Fatal(err)
	}
	state, _ := engine.PreviousState()
	if state.Epoch != 2 {
		t.Fatalf("epoch = %d, want 2", state.Epoch)
	}
}

func TestShadowEpochEngineRejectsTamperedPreview(t *testing.T) {
	engine, err := NewShadowEpochEngine(types.NetworkID{1}, testShadowEpochConfig())
	if err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(1, 0, 1_000, 500, 500)},
		[]compute.VerifiedWork{testVerifiedCPUWork(1, 200)},
		MonetaryBalanceSnapshot{TotalSupply: 1_000, StakedSupply: 400, ProtocolReserve: 100, BaseFee: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	preview.State.AggregateHash[0] ^= 0xff
	if err := engine.Accept(preview); err == nil {
		t.Fatal("tampered aggregate commitment accepted")
	}
}
