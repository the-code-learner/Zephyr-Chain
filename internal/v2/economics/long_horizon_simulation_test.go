package economics

import "testing"

func TestLongHorizonAdversarialShadowSimulation(t *testing.T) {
	cfg := DefaultLongHorizonSimulationConfig()
	inputs, err := BuildAdversarialLongHorizonInputs(3 * cfg.EpochsPerYear)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunLongHorizonShadowSimulation(cfg, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Shadow || len(result.Epochs) != int(3*cfg.EpochsPerYear) {
		t.Fatalf("unexpected long-horizon result shape: shadow=%v epochs=%d", result.Shadow, len(result.Epochs))
	}
	summary := result.Summary
	if summary.InitialTrackedCapital != 1_000_000 || summary.FinalTrackedCapital != 1_000_000 {
		t.Fatalf("simulation changed tracked capital: %#v", summary)
	}
	if summary.ReliableZPPIEpochs == 0 || summary.UnreliableZPPIEpochs == 0 {
		t.Fatalf("adversarial sparse-data mix did not exercise both ZPPI reliability states: %#v", summary)
	}
	if summary.DirectionUnavailableEpochs < cfg.EpochsPerYear ||
		summary.DirectionLooserEpochs+summary.DirectionOnTargetEpochs+summary.DirectionTighterEpochs == 0 {
		t.Fatalf("annual ZPPI direction gating was not exercised: %#v", summary)
	}
	if summary.ReliableScarcityEpochs == 0 || summary.UnreliableScarcityEpochs == 0 ||
		summary.PositiveScarcityEpochs == 0 || summary.NegativeScarcityEpochs == 0 ||
		summary.MaxAbsScarcityBps == 0 {
		t.Fatalf("oscillating scarcity scenario did not exercise expected regimes: %#v", summary)
	}
	if summary.MaxStateCarryingCostUnits <= result.Epochs[0].StateCarryingCostUnits {
		t.Fatalf("fragmentation pressure did not increase carrying cost: first=%d max=%d",
			result.Epochs[0].StateCarryingCostUnits, summary.MaxStateCarryingCostUnits)
	}
	for _, snapshot := range result.Epochs {
		if snapshot.TrackedCapital != summary.InitialTrackedCapital {
			t.Fatalf("epoch %d changed tracked capital: %d", snapshot.Epoch, snapshot.TrackedCapital)
		}
		if !snapshot.ZPPI.Reliable && snapshot.MonetaryDirection != ShadowDirectionUnavailable {
			t.Fatalf("epoch %d produced monetary direction from unreliable ZPPI: %#v", snapshot.Epoch, snapshot)
		}
	}

	firstProductive := result.Epochs[120]
	if firstProductive.ProductiveCoverageBps != 7_500 {
		t.Fatalf("unexpected first productive regime coverage: %#v", firstProductive)
	}
	if got := firstProductive.Dormancy.Buckets[0].Amount; got < 750_000 {
		t.Fatalf("productive regime did not reset expected capital age: got recent=%d", got)
	}
}

func TestLongHorizonAnnualDirectionRequiresReliableEndpoints(t *testing.T) {
	cfg := DefaultLongHorizonSimulationConfig()
	cfg.EpochsPerYear = 2
	cfg.DirectionBandBps = 0
	inputs := []LongHorizonEpochInput{
		longHorizonTestInput(true, ZPPIPriceScaleQ9),
		longHorizonTestInput(true, ZPPIPriceScaleQ9),
		longHorizonTestInput(false, ZPPIPriceScaleQ9),
		longHorizonTestInput(true, ZPPIPriceScaleQ9),
	}
	result, err := RunLongHorizonShadowSimulation(cfg, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if result.Epochs[2].MonetaryDirection != ShadowDirectionUnavailable {
		t.Fatalf("unreliable current endpoint produced annual direction: %#v", result.Epochs[2])
	}
	if result.Epochs[3].MonetaryDirection != ShadowDirectionLooser {
		t.Fatalf("flat reliable ZPPI should be below the annual target: %#v", result.Epochs[3])
	}
}

func TestLongHorizonSimulationRejectsCapitalMutationInputs(t *testing.T) {
	cfg := DefaultLongHorizonSimulationConfig()
	input := longHorizonTestInput(true, ZPPIPriceScaleQ9)
	input.ProductiveCoverageBps = BasisPoints + 1
	if _, err := RunLongHorizonShadowSimulation(cfg, []LongHorizonEpochInput{input}); err == nil {
		t.Fatal("invalid productive coverage accepted")
	}

	input = longHorizonTestInput(true, ZPPIPriceScaleQ9)
	input.FragmentCount = 0
	if _, err := RunLongHorizonShadowSimulation(cfg, []LongHorizonEpochInput{input}); err == nil {
		t.Fatal("zero fragmentation count accepted")
	}
}

func longHorizonTestInput(reliable bool, price uint64) LongHorizonEpochInput {
	return LongHorizonEpochInput{
		ZPPIObservations: []ZPPIObservation{
			{Component: ZPPICompute, PriceQ9: price, Reliable: reliable},
			{Component: ZPPIDataAvailability, PriceQ9: price, Reliable: reliable},
			{Component: ZPPIStorage, PriceQ9: price, Reliable: reliable},
		},
		ComputeMarket: ComputeMarketMetrics{
			EscrowBackedDemandUnits: 10_000,
			VerifiedSupplyUnits:     10_000,
			VerifiedSupplyReliable:  true,
			BacklogUnits:            1_000,
			FulfilledUnits:          9_000,
			UtilizationBps:          9_000,
			ComputeIndexReliable:    reliable,
		},
		FragmentCount:         1,
		PayloadBytesPerObject: 256,
	}
}

func TestLongHorizonSensitivityMatrixExercisesParameterBands(t *testing.T) {
	cfg := DefaultLongHorizonSimulationConfig()
	results, err := RunLongHorizonSensitivityMatrix(3*cfg.EpochsPerYear, DefaultLongHorizonSensitivityCases())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 7 {
		t.Fatalf("sensitivity result count = %d, want 7", len(results))
	}
	byName := make(map[string]LongHorizonSensitivityResult, len(results))
	for _, result := range results {
		if !result.Shadow || result.Summary.Epochs != 3*cfg.EpochsPerYear ||
			result.Summary.InitialTrackedCapital != result.Summary.FinalTrackedCapital {
			t.Fatalf("invalid sensitivity result %q: %#v", result.Name, result)
		}
		byName[result.Name] = result
	}

	baseline := byName["baseline"].Summary
	sparse := byName["sparse-evidence"].Summary
	strict := byName["strict-zppi-coverage"].Summary
	capped := byName["capped-scarcity"].Summary
	if sparse.UnreliableZPPIEpochs <= baseline.UnreliableZPPIEpochs {
		t.Fatalf("sparse-evidence case did not reduce ZPPI availability: baseline=%d sparse=%d",
			baseline.UnreliableZPPIEpochs, sparse.UnreliableZPPIEpochs)
	}
	if strict.UnreliableZPPIEpochs < baseline.UnreliableZPPIEpochs ||
		strict.ReliableZPPIEpochs > baseline.ReliableZPPIEpochs {
		t.Fatalf("strict ZPPI coverage unexpectedly improved reliability: baseline=%#v strict=%#v", baseline, strict)
	}
	if capped.MaxAbsScarcityBps > 5_000 || capped.MaxAbsScarcityBps > baseline.MaxAbsScarcityBps {
		t.Fatalf("scarcity cap was not respected: baseline=%d capped=%d",
			baseline.MaxAbsScarcityBps, capped.MaxAbsScarcityBps)
	}
	if byName["prolonged-idle"].Summary.MaxStateCarryingCostUnits != baseline.MaxStateCarryingCostUnits {
		t.Fatal("productive-regime sensitivity unexpectedly changed independent fragmentation cost pressure")
	}
}

func TestLongHorizonSensitivityRejectsDuplicateCaseNames(t *testing.T) {
	cfg := DefaultLongHorizonSimulationConfig()
	profile := DefaultLongHorizonAdversarialProfile()
	cases := []LongHorizonSensitivityCase{
		{Name: "duplicate", Config: cfg, Profile: profile},
		{Name: "duplicate", Config: cfg, Profile: profile},
	}
	if _, err := RunLongHorizonSensitivityMatrix(10, cases); err == nil {
		t.Fatal("duplicate sensitivity case names accepted")
	}
}
