package economics

import "errors"

var ErrLongHorizonSensitivity = errors.New("invalid Zephyr long-horizon sensitivity matrix")

type LongHorizonAdversarialProfile struct {
	ScarcityCycleEpochs       uint64
	HighScarcityEpochs        uint64
	ComputeGapEvery           uint64
	DataAvailabilityGapEvery  uint64
	StorageGapEvery           uint64
	CompoundGapEvery          uint64
	SupplyGapEvery            uint64
	ProductiveCycleEpochs     uint64
	IdleEpochs                uint64
	HighProductiveEpochs      uint64
	HighProductiveCoverageBps uint32
	ResidualCoverageBps       uint32
	FragmentStepEpochs        uint64
}

type LongHorizonSensitivityCase struct {
	Name    string
	Config  LongHorizonSimulationConfig
	Profile LongHorizonAdversarialProfile
}

type LongHorizonSensitivityResult struct {
	Name    string
	Shadow  bool
	Summary LongHorizonSimulationSummary
}

func DefaultLongHorizonAdversarialProfile() LongHorizonAdversarialProfile {
	return LongHorizonAdversarialProfile{
		ScarcityCycleEpochs:       120,
		HighScarcityEpochs:        60,
		ComputeGapEvery:           53,
		DataAvailabilityGapEvery:  17,
		StorageGapEvery:           29,
		CompoundGapEvery:          97,
		SupplyGapEvery:            41,
		ProductiveCycleEpochs:     365,
		IdleEpochs:                120,
		HighProductiveEpochs:      120,
		HighProductiveCoverageBps: 7_500,
		ResidualCoverageBps:       1_000,
		FragmentStepEpochs:        90,
	}
}

func BuildProfiledAdversarialLongHorizonInputs(
	epochs uint64,
	profile LongHorizonAdversarialProfile,
) ([]LongHorizonEpochInput, error) {
	if epochs == 0 || epochs > 1_000_000 || profile.ScarcityCycleEpochs == 0 ||
		profile.HighScarcityEpochs > profile.ScarcityCycleEpochs ||
		profile.ProductiveCycleEpochs == 0 || profile.IdleEpochs > profile.ProductiveCycleEpochs ||
		profile.HighProductiveEpochs > profile.ProductiveCycleEpochs-profile.IdleEpochs ||
		profile.HighProductiveCoverageBps > BasisPoints || profile.ResidualCoverageBps > BasisPoints ||
		profile.FragmentStepEpochs == 0 {
		return nil, ErrLongHorizonSimulation
	}

	out := make([]LongHorizonEpochInput, epochs)
	for i := uint64(0); i < epochs; i++ {
		highScarcity := i%profile.ScarcityCycleEpochs < profile.HighScarcityEpochs
		computePrice := uint64(900_000_000)
		daPrice := uint64(980_000_000)
		storagePrice := uint64(970_000_000)
		market := ComputeMarketMetrics{
			EscrowBackedDemandUnits: 10_000,
			VerifiedSupplyUnits:     20_000,
			VerifiedSupplyReliable:  !periodicGap(i, profile.SupplyGapEvery),
			BacklogUnits:            1_000,
			FulfilledUnits:          9_000,
			UtilizationBps:          4_500,
			ComputePriceTrendBps:    -800,
			ComputeIndexReliable:    true,
		}
		if highScarcity {
			computePrice = 1_100_000_000
			daPrice = 1_020_000_000
			storagePrice = 1_030_000_000
			market.EscrowBackedDemandUnits = 20_000
			market.VerifiedSupplyUnits = 10_000
			market.BacklogUnits = 11_000
			market.FulfilledUnits = 9_000
			market.UtilizationBps = 9_000
			market.ComputePriceTrendBps = 1_000
		}

		computeReliable := !periodicGap(i, profile.ComputeGapEvery)
		daReliable := !periodicGap(i, profile.DataAvailabilityGapEvery)
		storageReliable := !periodicGap(i, profile.StorageGapEvery)
		if periodicGap(i, profile.CompoundGapEvery) {
			daReliable = false
			storageReliable = false
		}
		market.ComputeIndexReliable = computeReliable

		coverage := profile.ResidualCoverageBps
		productivePhase := i % profile.ProductiveCycleEpochs
		switch {
		case productivePhase < profile.IdleEpochs:
			coverage = 0
		case productivePhase < profile.IdleEpochs+profile.HighProductiveEpochs:
			coverage = profile.HighProductiveCoverageBps
		}

		fragments := uint64(1)
		switch (i / profile.FragmentStepEpochs) % 4 {
		case 1:
			fragments = 10
		case 2:
			fragments = 100
		case 3:
			fragments = 1_000
		}

		out[i] = LongHorizonEpochInput{
			ZPPIObservations: []ZPPIObservation{
				{Component: ZPPICompute, PriceQ9: computePrice, Reliable: computeReliable},
				{Component: ZPPIDataAvailability, PriceQ9: daPrice, Reliable: daReliable},
				{Component: ZPPIStorage, PriceQ9: storagePrice, Reliable: storageReliable},
			},
			ComputeMarket:         market,
			ProductiveCoverageBps: coverage,
			FragmentCount:         fragments,
			PayloadBytesPerObject: 256,
		}
	}
	return out, nil
}

func DefaultLongHorizonSensitivityCases() []LongHorizonSensitivityCase {
	baseConfig := DefaultLongHorizonSimulationConfig()
	baseProfile := DefaultLongHorizonAdversarialProfile()

	sparse := baseProfile
	sparse.ComputeGapEvery = 11
	sparse.DataAvailabilityGapEvery = 5
	sparse.StorageGapEvery = 7
	sparse.CompoundGapEvery = 13
	sparse.SupplyGapEvery = 17

	strictCoverage := baseConfig
	strictCoverage.ZPPI.MinCoverageBps = BasisPoints

	fastEWMA := baseConfig
	fastEWMA.ZPPI.EWMABps = 5_000

	wideDirectionBand := baseConfig
	wideDirectionBand.DirectionBandBps = 100

	cappedScarcity := baseConfig
	cappedScarcity.ComputeScarcity.MaxAbsScoreBps = 5_000

	prolongedIdle := baseProfile
	prolongedIdle.IdleEpochs = 240
	prolongedIdle.HighProductiveEpochs = 60
	prolongedIdle.HighProductiveCoverageBps = 5_000
	prolongedIdle.ResidualCoverageBps = 0

	return []LongHorizonSensitivityCase{
		{Name: "baseline", Config: baseConfig, Profile: baseProfile},
		{Name: "sparse-evidence", Config: baseConfig, Profile: sparse},
		{Name: "strict-zppi-coverage", Config: strictCoverage, Profile: baseProfile},
		{Name: "fast-zppi-ewma", Config: fastEWMA, Profile: baseProfile},
		{Name: "wide-direction-band", Config: wideDirectionBand, Profile: baseProfile},
		{Name: "capped-scarcity", Config: cappedScarcity, Profile: baseProfile},
		{Name: "prolonged-idle", Config: baseConfig, Profile: prolongedIdle},
	}
}

func RunLongHorizonSensitivityMatrix(
	epochs uint64,
	cases []LongHorizonSensitivityCase,
) ([]LongHorizonSensitivityResult, error) {
	if epochs == 0 || len(cases) == 0 {
		return nil, ErrLongHorizonSensitivity
	}
	seen := make(map[string]struct{}, len(cases))
	out := make([]LongHorizonSensitivityResult, 0, len(cases))
	for _, scenario := range cases {
		if scenario.Name == "" {
			return nil, ErrLongHorizonSensitivity
		}
		if _, duplicate := seen[scenario.Name]; duplicate {
			return nil, ErrLongHorizonSensitivity
		}
		seen[scenario.Name] = struct{}{}
		inputs, err := BuildProfiledAdversarialLongHorizonInputs(epochs, scenario.Profile)
		if err != nil {
			return nil, err
		}
		result, err := RunLongHorizonShadowSimulation(scenario.Config, inputs)
		if err != nil {
			return nil, err
		}
		if !result.Shadow {
			return nil, ErrLongHorizonSensitivity
		}
		out = append(out, LongHorizonSensitivityResult{
			Name: scenario.Name, Shadow: true, Summary: result.Summary,
		})
	}
	return out, nil
}

func periodicGap(epochIndex, every uint64) bool {
	return every > 0 && epochIndex%every == 0
}
