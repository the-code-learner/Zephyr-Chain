package economics

import "testing"

func TestShadowMonetaryControllerOffsetsBurnAndTargetsNetIssuance(t *testing.T) {
	policy := DefaultShadowPolicy()
	metrics := MonetaryMetrics{
		Supply:                 1_000_000_000,
		CirculatingSupply:      900_000_000,
		StakedSupply:           450_000_000,
		ProtocolReserve:        100_000_000,
		BurnedThisEpoch:        12_345,
		FinalizedOperations:    policy.OperationsTarget,
		ResourceUtilizationBps: policy.UtilizationTargetBps,
		AgeWeightedVelocityBps: policy.VelocityTargetBps,
		ComputeIndexQ9:         7_500_000_000,
		ComputePriceTrendBps:   250,
		ComputeIndexReliable:   true,
	}
	decision, err := EvaluateShadow(policy.TargetInflationBps, metrics, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Shadow || decision.TargetInflationBps != policy.TargetInflationBps {
		t.Fatalf("unexpected target decision: %#v", decision)
	}
	if decision.GrossMintTarget != decision.NetIssuanceTarget+metrics.BurnedThisEpoch || decision.ProjectedNetChange != decision.NetIssuanceTarget {
		t.Fatalf("burn must be offset before net inflation target: %#v", decision)
	}
}

func TestShadowMonetaryControllerRateLimitsAdaptiveTarget(t *testing.T) {
	policy := DefaultShadowPolicy()
	metrics := MonetaryMetrics{
		Supply:                 1_000_000_000,
		CirculatingSupply:      900_000_000,
		StakedSupply:           100_000_000,
		ProtocolReserve:        1_000_000,
		FinalizedOperations:    1,
		ResourceUtilizationBps: 100,
		AgeWeightedVelocityBps: 100,
	}
	decision, err := EvaluateShadow(200, metrics, policy)
	if err != nil {
		t.Fatal(err)
	}
	if decision.TargetInflationBps != 201 {
		t.Fatalf("expected one-basis-point upward rate limit, got %d", decision.TargetInflationBps)
	}
}

func TestComputeIndexIsTelemetryOnlyInShadowV0(t *testing.T) {
	policy := DefaultShadowPolicy()
	base := MonetaryMetrics{
		Supply:                 1_000_000_000,
		CirculatingSupply:      900_000_000,
		StakedSupply:           450_000_000,
		ProtocolReserve:        100_000_000,
		FinalizedOperations:    policy.OperationsTarget,
		ResourceUtilizationBps: policy.UtilizationTargetBps,
		AgeWeightedVelocityBps: policy.VelocityTargetBps,
	}
	first, err := EvaluateShadow(200, base, policy)
	if err != nil {
		t.Fatal(err)
	}
	base.ComputeIndexQ9 = 99_000_000_000
	base.ComputePriceTrendBps = 10_000
	base.ComputeIndexReliable = true
	second, err := EvaluateShadow(200, base, policy)
	if err != nil {
		t.Fatal(err)
	}
	if first.TargetInflationBps != second.TargetInflationBps {
		t.Fatalf("compute price must remain telemetry-only before activation study: %d != %d", first.TargetInflationBps, second.TargetInflationBps)
	}
}
