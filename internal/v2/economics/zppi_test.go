package economics

import "testing"

func TestBuildZPPIChainWeightedBasket(t *testing.T) {
	cfg := ZPPIBasketConfig{MinCoverageBps: 8_000, EWMABps: BasisPoints}
	cfg.WeightsBps[ZPPICompute] = 5_000
	cfg.WeightsBps[ZPPIDataAvailability] = 3_000
	cfg.WeightsBps[ZPPIStorage] = 2_000
	cfg.ReferencePriceQ9[ZPPICompute] = 100 * ZPPIPriceScaleQ9
	cfg.ReferencePriceQ9[ZPPIDataAvailability] = 20 * ZPPIPriceScaleQ9
	cfg.ReferencePriceQ9[ZPPIStorage] = 10 * ZPPIPriceScaleQ9

	snapshot, err := BuildZPPI(1, []ZPPIObservation{
		{Component: ZPPICompute, PriceQ9: 102 * ZPPIPriceScaleQ9, Reliable: true},
		{Component: ZPPIDataAvailability, PriceQ9: 20 * ZPPIPriceScaleQ9, Reliable: true},
		{Component: ZPPIStorage, PriceQ9: 10 * ZPPIPriceScaleQ9, Reliable: false},
	}, ZPPISnapshot{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Reliable || snapshot.CoverageBps != 8_000 {
		t.Fatalf("coverage/reliability = %d/%v", snapshot.CoverageBps, snapshot.Reliable)
	}
	if got, want := snapshot.BasketIndexQ9, uint64(1_012_500_000); got != want {
		t.Fatalf("basket index = %d, want %d", got, want)
	}
}

func TestPurchasingPowerTargetConversion(t *testing.T) {
	target, err := TargetZPPIFromPrior(ZPPIPriceScaleQ9)
	if err != nil {
		t.Fatal(err)
	}
	if target != TargetZPPIAnnualFactorQ9 {
		t.Fatalf("target = %d, want %d", target, TargetZPPIAnnualFactorQ9)
	}
	power, err := PurchasingPowerQ9(target)
	if err != nil {
		t.Fatal(err)
	}
	if power < 979_999_999 || power > 980_000_001 {
		t.Fatalf("purchasing power = %d, want approximately 980000000", power)
	}
}

func TestBuildZPPIFailsClosedOnLowCoverage(t *testing.T) {
	cfg := ZPPIBasketConfig{MinCoverageBps: 7_500, EWMABps: 5_000}
	cfg.WeightsBps[ZPPICompute] = 5_000
	cfg.WeightsBps[ZPPIStorage] = 5_000
	cfg.ReferencePriceQ9[ZPPICompute] = ZPPIPriceScaleQ9
	cfg.ReferencePriceQ9[ZPPIStorage] = ZPPIPriceScaleQ9

	snapshot, err := BuildZPPI(1, []ZPPIObservation{{Component: ZPPICompute, PriceQ9: ZPPIPriceScaleQ9, Reliable: true}}, ZPPISnapshot{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Reliable || snapshot.CoverageBps != 5_000 {
		t.Fatalf("low coverage must remain unreliable: %d/%v", snapshot.CoverageBps, snapshot.Reliable)
	}
}

func TestBuildZPPIPreservesEWMAHistoryAcrossMissingComponent(t *testing.T) {
	cfg := ZPPIBasketConfig{MinCoverageBps: 7_500, EWMABps: 5_000}
	cfg.WeightsBps[ZPPICompute] = 5_000
	cfg.WeightsBps[ZPPIStorage] = 5_000
	cfg.ReferencePriceQ9[ZPPICompute] = ZPPIPriceScaleQ9
	cfg.ReferencePriceQ9[ZPPIStorage] = ZPPIPriceScaleQ9

	first, err := BuildZPPI(1, []ZPPIObservation{
		{Component: ZPPICompute, PriceQ9: ZPPIPriceScaleQ9, Reliable: true},
		{Component: ZPPIStorage, PriceQ9: ZPPIPriceScaleQ9, Reliable: true},
	}, ZPPISnapshot{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sparse, err := BuildZPPI(2, []ZPPIObservation{
		{Component: ZPPIStorage, PriceQ9: ZPPIPriceScaleQ9, Reliable: true},
	}, first, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if sparse.Reliable || sparse.CoverageBps != 5_000 {
		t.Fatalf("sparse epoch unexpectedly reliable: %#v", sparse)
	}
	if sparse.ComponentIndexQ9[ZPPICompute] != ZPPIPriceScaleQ9 {
		t.Fatalf("sparse epoch lost compute EWMA history: %#v", sparse)
	}

	recovered, err := BuildZPPI(3, []ZPPIObservation{
		{Component: ZPPICompute, PriceQ9: 2 * ZPPIPriceScaleQ9, Reliable: true},
		{Component: ZPPIStorage, PriceQ9: ZPPIPriceScaleQ9, Reliable: true},
	}, sparse, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Reliable {
		t.Fatalf("recovered epoch remained unreliable: %#v", recovered)
	}
	if got, want := recovered.ComponentIndexQ9[ZPPICompute], uint64(1_500_000_000); got != want {
		t.Fatalf("compute EWMA after sparse epoch = %d, want %d", got, want)
	}
}
