package economics

import "testing"

func TestComputeScarcityRisesWhenVerifiedDemandExceedsSupply(t *testing.T) {
	metrics := ComputeMarketMetrics{
		EscrowBackedDemandUnits: 2_000,
		VerifiedSupplyUnits:     1_000,
		BacklogUnits:            500,
		FulfilledUnits:          1_500,
		UtilizationBps:          9_000,
		ComputePriceTrendBps:    1_000,
		ComputeIndexReliable:    true,
	}
	snapshot, err := BuildComputeScarcity(1, metrics, DefaultComputeScarcityConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Reliable || snapshot.ScoreBps <= 0 {
		t.Fatalf("expected reliable positive scarcity, got %#v", snapshot)
	}
	if snapshot.DemandSupplyPressureBps != int32(BasisPoints) {
		t.Fatalf("expected demand/supply pressure clamp, got %d", snapshot.DemandSupplyPressureBps)
	}
}

func TestComputeScarcityIgnoresUnreliablePriceSignal(t *testing.T) {
	cfg := DefaultComputeScarcityConfig()
	metrics := ComputeMarketMetrics{
		EscrowBackedDemandUnits: 2_000,
		VerifiedSupplyUnits:     2_000,
		FulfilledUnits:          2_000,
		UtilizationBps:          cfg.UtilizationTargetBps,
		ComputePriceTrendBps:    10_000,
		ComputeIndexReliable:    false,
	}
	first, err := BuildComputeScarcity(1, metrics, cfg)
	if err != nil {
		t.Fatal(err)
	}
	metrics.ComputePriceTrendBps = -10_000
	second, err := BuildComputeScarcity(2, metrics, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.ScoreBps != second.ScoreBps || first.PriceTrendPressureBps != 0 || second.PriceTrendPressureBps != 0 {
		t.Fatalf("unreliable ZCPI changed scarcity: %#v %#v", first, second)
	}
}

func TestComputeScarcityRejectsImpossibleSettlementMetrics(t *testing.T) {
	metrics := ComputeMarketMetrics{EscrowBackedDemandUnits: 100, VerifiedSupplyUnits: 100, BacklogUnits: 101}
	if _, err := BuildComputeScarcity(1, metrics, DefaultComputeScarcityConfig()); err != ErrComputeScarcity {
		t.Fatalf("expected invalid backlog rejection, got %v", err)
	}
}

func TestComputeFeedbackModesKeepActivationShadowed(t *testing.T) {
	monetary := DefaultShadowPolicy()
	metrics := MonetaryMetrics{
		Supply:                 1_000_000_000,
		CirculatingSupply:      900_000_000,
		StakedSupply:           450_000_000,
		ProtocolReserve:        100_000_000,
		BurnedThisEpoch:        10_000,
		FinalizedOperations:    monetary.OperationsTarget,
		ResourceUtilizationBps: monetary.UtilizationTargetBps,
		AgeWeightedVelocityBps: monetary.VelocityTargetBps,
	}
	base, err := EvaluateShadow(monetary.TargetInflationBps, metrics, monetary)
	if err != nil {
		t.Fatal(err)
	}
	scarcity := ComputeScarcitySnapshot{Epoch: 1, ScoreBps: 8_000, Reliable: true}

	observe, err := EvaluateComputeFeedback(base, metrics, monetary, scarcity, DefaultComputeFeedbackPolicy(ComputeFeedbackObserveOnly))
	if err != nil {
		t.Fatal(err)
	}
	if observe.SuggestedTargetInflationBps != base.TargetInflationBps || observe.ComputeRewardShareBps != 1_000 {
		t.Fatalf("observe-only mode altered policy: %#v", observe)
	}

	routing, err := EvaluateComputeFeedback(base, metrics, monetary, scarcity, DefaultComputeFeedbackPolicy(ComputeFeedbackRewardRouting))
	if err != nil {
		t.Fatal(err)
	}
	if routing.ComputeRewardShareBps <= observe.ComputeRewardShareBps || routing.SuggestedTargetInflationBps != base.TargetInflationBps {
		t.Fatalf("reward-routing mode did not isolate compute allocation: %#v", routing)
	}

	band, err := EvaluateComputeFeedback(base, metrics, monetary, scarcity, DefaultComputeFeedbackPolicy(ComputeFeedbackMonetaryBand))
	if err != nil {
		t.Fatal(err)
	}
	if !band.Shadow || band.InflationCorrectionBps <= 0 || band.SuggestedTargetInflationBps <= base.TargetInflationBps {
		t.Fatalf("monetary-band mode did not produce bounded shadow correction: %#v", band)
	}
}

func TestUnreliableScarcityCannotMoveComputePolicy(t *testing.T) {
	monetary := DefaultShadowPolicy()
	metrics := MonetaryMetrics{
		Supply:                 1_000_000_000,
		CirculatingSupply:      900_000_000,
		StakedSupply:           450_000_000,
		ProtocolReserve:        100_000_000,
		FinalizedOperations:    monetary.OperationsTarget,
		ResourceUtilizationBps: monetary.UtilizationTargetBps,
		AgeWeightedVelocityBps: monetary.VelocityTargetBps,
	}
	base, err := EvaluateShadow(200, metrics, monetary)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := EvaluateComputeFeedback(base, metrics, monetary, ComputeScarcitySnapshot{Epoch: 1, ScoreBps: 10_000, Reliable: false}, DefaultComputeFeedbackPolicy(ComputeFeedbackMonetaryBand))
	if err != nil {
		t.Fatal(err)
	}
	if decision.InflationCorrectionBps != 0 || decision.SuggestedTargetInflationBps != base.TargetInflationBps || decision.ComputeRewardShareBps != 1_000 {
		t.Fatalf("unreliable scarcity moved policy: %#v", decision)
	}
}
