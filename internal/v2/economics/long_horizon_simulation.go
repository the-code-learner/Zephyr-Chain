package economics

import (
	"errors"
	"math"
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrLongHorizonSimulation = errors.New("invalid Zephyr long-horizon shadow simulation")

type ShadowMonetaryDirection string

const (
	ShadowDirectionUnavailable ShadowMonetaryDirection = "unavailable"
	ShadowDirectionLooser      ShadowMonetaryDirection = "looser"
	ShadowDirectionOnTarget    ShadowMonetaryDirection = "on-target"
	ShadowDirectionTighter     ShadowMonetaryDirection = "tighter"
)

type LongHorizonSimulationConfig struct {
	EpochsPerYear       uint64
	DirectionBandBps    uint32
	InitialHeight       uint64
	BlocksPerEpoch      uint64
	ZPPI                ZPPIBasketConfig
	ComputeScarcity     ComputeScarcityConfig
	StateCarryingCost   StateCarryingCostPolicy
	DormancyUpperBounds []uint64
	InitialCapital      []CapitalLot
}

type LongHorizonEpochInput struct {
	ZPPIObservations      []ZPPIObservation
	ComputeMarket         ComputeMarketMetrics
	ProductiveCoverageBps uint32
	FragmentCount         uint64
	PayloadBytesPerObject uint64
}

type LongHorizonEpochSnapshot struct {
	Epoch                  uint64
	Height                 uint64
	ZPPI                   ZPPISnapshot
	PurchasingPowerQ9      uint64
	AnnualTargetZPPIQ9     uint64
	MonetaryDirection      ShadowMonetaryDirection
	ComputeScarcity        ComputeScarcitySnapshot
	ProductiveCoverageBps  uint32
	Dormancy               DormancyHistogram
	FragmentCount          uint64
	StateCarryingCostUnits uint64
	TrackedCapital         uint64
}

type LongHorizonSimulationSummary struct {
	Epochs                     uint64
	ReliableZPPIEpochs         uint64
	UnreliableZPPIEpochs       uint64
	DirectionUnavailableEpochs uint64
	DirectionLooserEpochs      uint64
	DirectionOnTargetEpochs    uint64
	DirectionTighterEpochs     uint64
	ReliableScarcityEpochs     uint64
	UnreliableScarcityEpochs   uint64
	PositiveScarcityEpochs     uint64
	NeutralScarcityEpochs      uint64
	NegativeScarcityEpochs     uint64
	MaxAbsScarcityBps          uint32
	MaxStateCarryingCostUnits  uint64
	InitialTrackedCapital      uint64
	FinalTrackedCapital        uint64
	FinalZPPIQ9                uint64
	FinalPurchasingPowerQ9     uint64
	FinalDormancy              DormancyHistogram
}

type LongHorizonSimulationResult struct {
	Shadow  bool
	Epochs  []LongHorizonEpochSnapshot
	Summary LongHorizonSimulationSummary
}

func DefaultLongHorizonSimulationConfig() LongHorizonSimulationConfig {
	zppi := ZPPIBasketConfig{MinCoverageBps: 8_000, EWMABps: 2_000}
	zppi.WeightsBps[ZPPICompute] = 5_000
	zppi.WeightsBps[ZPPIDataAvailability] = 3_000
	zppi.WeightsBps[ZPPIStorage] = 2_000
	zppi.ReferencePriceQ9[ZPPICompute] = ZPPIPriceScaleQ9
	zppi.ReferencePriceQ9[ZPPIDataAvailability] = ZPPIPriceScaleQ9
	zppi.ReferencePriceQ9[ZPPIStorage] = ZPPIPriceScaleQ9

	return LongHorizonSimulationConfig{
		EpochsPerYear:    365,
		DirectionBandBps: 25,
		InitialHeight:    1,
		BlocksPerEpoch:   1,
		ZPPI:             zppi,
		ComputeScarcity:  DefaultComputeScarcityConfig(),
		StateCarryingCost: StateCarryingCostPolicy{
			BaseUnitsPerObject: 100,
			UnitsPerKiB:        10,
		},
		DormancyUpperBounds: []uint64{30, 180, 365, 730},
		InitialCapital: []CapitalLot{{
			LineageID:       types.HashBytes("economics/long-horizon/capital/v1", []byte("seed")),
			Amount:          1_000_000,
			IdleSinceHeight: 1,
		}},
	}
}

func BuildAdversarialLongHorizonInputs(epochs uint64) ([]LongHorizonEpochInput, error) {
	return BuildProfiledAdversarialLongHorizonInputs(epochs, DefaultLongHorizonAdversarialProfile())
}

func RunLongHorizonShadowSimulation(
	config LongHorizonSimulationConfig,
	inputs []LongHorizonEpochInput,
) (LongHorizonSimulationResult, error) {
	if len(inputs) == 0 || config.EpochsPerYear == 0 || config.DirectionBandBps > BasisPoints ||
		config.InitialHeight == 0 || config.BlocksPerEpoch == 0 ||
		config.StateCarryingCost.BaseUnitsPerObject == 0 || config.StateCarryingCost.UnitsPerKiB == 0 ||
		len(config.DormancyUpperBounds) == 0 || len(config.InitialCapital) == 0 {
		return LongHorizonSimulationResult{}, ErrLongHorizonSimulation
	}

	lots, err := compactCapitalLots(config.InitialCapital)
	if err != nil {
		return LongHorizonSimulationResult{}, err
	}
	initialCapital, err := capitalLotsTotal(lots)
	if err != nil {
		return LongHorizonSimulationResult{}, err
	}
	for _, lot := range lots {
		if lot.IdleSinceHeight > config.InitialHeight {
			return LongHorizonSimulationResult{}, ErrLongHorizonSimulation
		}
	}
	if uint64(len(inputs)) > (math.MaxUint64-config.InitialHeight)/config.BlocksPerEpoch {
		return LongHorizonSimulationResult{}, ErrLongHorizonSimulation
	}

	result := LongHorizonSimulationResult{
		Shadow: true,
		Epochs: make([]LongHorizonEpochSnapshot, 0, len(inputs)),
		Summary: LongHorizonSimulationSummary{
			Epochs:                uint64(len(inputs)),
			InitialTrackedCapital: initialCapital,
		},
	}
	var priorZPPI ZPPISnapshot

	for i, input := range inputs {
		epoch := uint64(i + 1)
		height := config.InitialHeight + epoch*config.BlocksPerEpoch
		if input.ProductiveCoverageBps > BasisPoints || input.FragmentCount == 0 || input.PayloadBytesPerObject == 0 ||
			input.FragmentCount > math.MaxUint64/input.PayloadBytesPerObject {
			return LongHorizonSimulationResult{}, ErrLongHorizonSimulation
		}

		zppi, err := BuildZPPI(epoch, input.ZPPIObservations, priorZPPI, config.ZPPI)
		if err != nil {
			return LongHorizonSimulationResult{}, err
		}
		purchasingPower := uint64(0)
		if zppi.BasketIndexQ9 != 0 {
			purchasingPower, err = PurchasingPowerQ9(zppi.BasketIndexQ9)
			if err != nil {
				return LongHorizonSimulationResult{}, err
			}
		}

		target := uint64(0)
		direction := ShadowDirectionUnavailable
		if uint64(i) >= config.EpochsPerYear {
			baseline := result.Epochs[uint64(i)-config.EpochsPerYear].ZPPI
			if baseline.Reliable && zppi.Reliable && baseline.BasketIndexQ9 != 0 && zppi.BasketIndexQ9 != 0 {
				target, err = TargetZPPIFromPrior(baseline.BasketIndexQ9)
				if err != nil {
					return LongHorizonSimulationResult{}, err
				}
				direction, err = shadowDirectionForZPPI(zppi.BasketIndexQ9, target, config.DirectionBandBps)
				if err != nil {
					return LongHorizonSimulationResult{}, err
				}
			}
		}

		scarcity, err := BuildComputeScarcity(epoch, input.ComputeMarket, config.ComputeScarcity)
		if err != nil {
			return LongHorizonSimulationResult{}, err
		}

		if input.ProductiveCoverageBps > 0 {
			lots, err = MarkProductiveCoverage(lots, height, input.ProductiveCoverageBps)
			if err != nil {
				return LongHorizonSimulationResult{}, err
			}
		}
		dormancy, err := BuildDormancyHistogram(lots, height, config.DormancyUpperBounds)
		if err != nil {
			return LongHorizonSimulationResult{}, err
		}
		trackedCapital, err := capitalLotsTotal(lots)
		if err != nil || trackedCapital != initialCapital {
			return LongHorizonSimulationResult{}, ErrLongHorizonSimulation
		}
		payloadBytes := input.FragmentCount * input.PayloadBytesPerObject
		carryingCost, err := EstimateStateCarryingCost(input.FragmentCount, payloadBytes, config.StateCarryingCost)
		if err != nil {
			return LongHorizonSimulationResult{}, err
		}

		snapshot := LongHorizonEpochSnapshot{
			Epoch:                  epoch,
			Height:                 height,
			ZPPI:                   zppi,
			PurchasingPowerQ9:      purchasingPower,
			AnnualTargetZPPIQ9:     target,
			MonetaryDirection:      direction,
			ComputeScarcity:        scarcity,
			ProductiveCoverageBps:  input.ProductiveCoverageBps,
			Dormancy:               dormancy,
			FragmentCount:          input.FragmentCount,
			StateCarryingCostUnits: carryingCost,
			TrackedCapital:         trackedCapital,
		}
		result.Epochs = append(result.Epochs, snapshot)
		updateLongHorizonSummary(&result.Summary, snapshot)
		priorZPPI = zppi
	}

	last := result.Epochs[len(result.Epochs)-1]
	result.Summary.FinalTrackedCapital = last.TrackedCapital
	result.Summary.FinalZPPIQ9 = last.ZPPI.BasketIndexQ9
	result.Summary.FinalPurchasingPowerQ9 = last.PurchasingPowerQ9
	result.Summary.FinalDormancy = last.Dormancy
	return result, nil
}

func shadowDirectionForZPPI(current, target uint64, bandBps uint32) (ShadowMonetaryDirection, error) {
	if current == 0 || target == 0 || bandBps > BasisPoints {
		return ShadowDirectionUnavailable, ErrLongHorizonSimulation
	}
	lower := new(big.Int).Mul(new(big.Int).SetUint64(target), new(big.Int).SetUint64(uint64(BasisPoints-bandBps)))
	lower.Quo(lower, new(big.Int).SetUint64(uint64(BasisPoints)))
	upper := new(big.Int).Mul(new(big.Int).SetUint64(target), new(big.Int).SetUint64(uint64(BasisPoints+bandBps)))
	upper.Quo(upper, new(big.Int).SetUint64(uint64(BasisPoints)))
	if !lower.IsUint64() || !upper.IsUint64() {
		return ShadowDirectionUnavailable, ErrLongHorizonSimulation
	}
	if current < lower.Uint64() {
		return ShadowDirectionLooser, nil
	}
	if current > upper.Uint64() {
		return ShadowDirectionTighter, nil
	}
	return ShadowDirectionOnTarget, nil
}

func capitalLotsTotal(lots []CapitalLot) (uint64, error) {
	total := new(big.Int)
	for _, lot := range lots {
		if err := lot.Validate(); err != nil {
			return 0, err
		}
		total.Add(total, new(big.Int).SetUint64(lot.Amount))
	}
	if !total.IsUint64() || total.Sign() <= 0 {
		return 0, ErrLongHorizonSimulation
	}
	return total.Uint64(), nil
}

func updateLongHorizonSummary(summary *LongHorizonSimulationSummary, snapshot LongHorizonEpochSnapshot) {
	if summary == nil {
		return
	}
	if snapshot.ZPPI.Reliable {
		summary.ReliableZPPIEpochs++
	} else {
		summary.UnreliableZPPIEpochs++
	}
	switch snapshot.MonetaryDirection {
	case ShadowDirectionLooser:
		summary.DirectionLooserEpochs++
	case ShadowDirectionOnTarget:
		summary.DirectionOnTargetEpochs++
	case ShadowDirectionTighter:
		summary.DirectionTighterEpochs++
	default:
		summary.DirectionUnavailableEpochs++
	}
	if snapshot.ComputeScarcity.Reliable {
		summary.ReliableScarcityEpochs++
	} else {
		summary.UnreliableScarcityEpochs++
	}
	switch {
	case snapshot.ComputeScarcity.ScoreBps > 0:
		summary.PositiveScarcityEpochs++
	case snapshot.ComputeScarcity.ScoreBps < 0:
		summary.NegativeScarcityEpochs++
	default:
		summary.NeutralScarcityEpochs++
	}
	absScarcity := int64(snapshot.ComputeScarcity.ScoreBps)
	if absScarcity < 0 {
		absScarcity = -absScarcity
	}
	if uint32(absScarcity) > summary.MaxAbsScarcityBps {
		summary.MaxAbsScarcityBps = uint32(absScarcity)
	}
	if snapshot.StateCarryingCostUnits > summary.MaxStateCarryingCostUnits {
		summary.MaxStateCarryingCostUnits = snapshot.StateCarryingCostUnits
	}
}
