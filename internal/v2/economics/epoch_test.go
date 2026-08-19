package economics

import "testing"

func TestAggregateEpochMetricsSeparatesChainAndComputeUtilization(t *testing.T) {
	metrics := []ShardEpochMetrics{
		{
			Version: EpochMetricsVersion, Epoch: 7, ShardID: 0,
			ChargedFees: 100, BurnedFees: 40, ValidatorFees: 50, ReserveFees: 10,
			FinalizedOperations: 1_000, ResourceUsed: 50, ResourceCapacity: 100,
			CirculatingNativeSupply: 800, AgeWeightedVelocityBps: 2_000,
			EscrowBackedComputeDemand: 100, VerifiedComputeSupply: 80,
			ComputeFulfilled: 70, ComputeExpired: 10, ComputeBacklog: 20,
		},
		{
			Version: EpochMetricsVersion, Epoch: 7, ShardID: 1,
			ChargedFees: 50, BurnedFees: 20, ValidatorFees: 25, ReserveFees: 5,
			FinalizedOperations: 500, ResourceUsed: 10, ResourceCapacity: 100,
			CirculatingNativeSupply: 200, AgeWeightedVelocityBps: 8_000,
			EscrowBackedComputeDemand: 50, VerifiedComputeSupply: 40,
			ComputeFulfilled: 30, ComputeExpired: 10, ComputeBacklog: 10,
		},
	}
	aggregate, err := AggregateEpochMetrics(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ChargedFees != 150 || aggregate.BurnedFees != 60 || aggregate.FinalizedOperations != 1_500 {
		t.Fatalf("unexpected totals: %#v", aggregate)
	}
	if aggregate.ResourceUtilizationBps != 3_000 {
		t.Fatalf("chain utilization should be 3000 bps, got %d", aggregate.ResourceUtilizationBps)
	}
	if aggregate.ComputeUtilizationBps != 8_333 {
		t.Fatalf("compute utilization should be based on fulfilled/capacity, got %d", aggregate.ComputeUtilizationBps)
	}
	if aggregate.AgeWeightedVelocityBps != 3_200 {
		t.Fatalf("velocity must be supply-weighted across shards, got %d", aggregate.AgeWeightedVelocityBps)
	}
	if aggregate.ComputeExpired != 20 {
		t.Fatalf("expired compute = %d, want 20", aggregate.ComputeExpired)
	}
	market := aggregate.ComputeMarketMetrics(500, true)
	if market.UtilizationBps != aggregate.ComputeUtilizationBps || market.EscrowBackedDemandUnits != 150 || market.VerifiedSupplyUnits != 120 {
		t.Fatalf("unexpected compute market projection: %#v", market)
	}
}

func TestShardEpochMetricsCarriesBacklogAcrossEpochs(t *testing.T) {
	metrics := ShardEpochMetrics{
		Version: EpochMetricsVersion, Epoch: 2, ResourceCapacity: 100,
		OpeningComputeBacklog: 100, EscrowBackedComputeDemand: 20, VerifiedComputeSupply: 100,
		ComputeFulfilled: 80, ComputeExpired: 10, ComputeBacklog: 30,
	}
	if err := metrics.Validate(); err != nil {
		t.Fatal(err)
	}
	market, err := AggregateEpochMetrics([]ShardEpochMetrics{metrics})
	if err != nil {
		t.Fatal(err)
	}
	projected := market.ComputeMarketMetrics(0, false)
	if projected.EscrowBackedDemandUnits != 120 || projected.BacklogUnits != 30 || projected.FulfilledUnits != 80 {
		t.Fatalf("unexpected carried demand: %#v", projected)
	}
}

func TestShardEpochMetricsRejectsInconsistentAccounting(t *testing.T) {
	base := ShardEpochMetrics{
		Version: EpochMetricsVersion, Epoch: 1, ResourceCapacity: 100,
		ChargedFees: 10, BurnedFees: 4, ValidatorFees: 5, ReserveFees: 1,
		EscrowBackedComputeDemand: 100, VerifiedComputeSupply: 100, ComputeFulfilled: 60, ComputeBacklog: 40,
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	badFees := base
	badFees.ReserveFees = 2
	if err := badFees.Validate(); err != ErrEpochMetrics {
		t.Fatalf("fee accounting mismatch accepted: %v", err)
	}
	badDemand := base
	badDemand.ComputeBacklog = 41
	if err := badDemand.Validate(); err != ErrEpochMetrics {
		t.Fatalf("broken compute flow accepted: %v", err)
	}
}

func TestAggregateEpochMetricsRejectsDuplicateShard(t *testing.T) {
	base := ShardEpochMetrics{
		Version: EpochMetricsVersion, Epoch: 1, ShardID: 0,
		ResourceCapacity: 1, ChargedFees: 1, BurnedFees: 1,
	}
	if _, err := AggregateEpochMetrics([]ShardEpochMetrics{base, base}); err != ErrEpochMetrics {
		t.Fatalf("duplicate shard accepted: %v", err)
	}
}

func TestEpochAggregateBuildsZAMPInputs(t *testing.T) {
	aggregate := EpochAggregate{
		Epoch: 1, ShardCount: 1, BurnedFees: 123, FinalizedOperations: 100,
		ResourceUtilizationBps: 4_000, CirculatingNativeSupply: 900, AgeWeightedVelocityBps: 3_000,
	}
	metrics, err := aggregate.MonetaryMetrics(1_000, 400, 50, 7_500_000_000, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.BurnedThisEpoch != 123 || metrics.StakedSupply != 400 || metrics.AgeWeightedVelocityBps != 3_000 || metrics.ComputeIndexQ9 != 7_500_000_000 {
		t.Fatalf("unexpected monetary projection: %#v", metrics)
	}
}
