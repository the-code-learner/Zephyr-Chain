package economics

import (
	"errors"
	"math/big"
)

const (
	ZPPIPriceScaleQ9                 uint64 = 1_000_000_000
	PurchasingPowerRetentionAnnualQ9 uint64 = 980_000_000
	TargetZPPIAnnualFactorQ9         uint64 = 1_020_408_163
)

var ErrZPPI = errors.New("invalid Zephyr purchasing power index input")

type ZPPIComponent uint8

const (
	ZPPIUnknown ZPPIComponent = iota
	ZPPICompute
	ZPPIDataAvailability
	ZPPIStorage
	ZPPIComponentCount
)

type ZPPIBasketConfig struct {
	WeightsBps       [ZPPIComponentCount]uint32
	ReferencePriceQ9 [ZPPIComponentCount]uint64
	MinCoverageBps   uint32
	EWMABps          uint32
}

type ZPPIObservation struct {
	Component ZPPIComponent
	PriceQ9   uint64
	Reliable  bool
}

type ZPPISnapshot struct {
	Epoch            uint64
	ComponentIndexQ9 [ZPPIComponentCount]uint64
	BasketIndexQ9    uint64
	CoverageBps      uint32
	Reliable         bool
}

// BuildZPPI constructs a version-scoped, chain-weighted price-relative basket.
// Each component is normalized to the reference price fixed when the basket
// version activates, so heterogeneous services can be combined without treating
// their raw prices as directly interchangeable.
func BuildZPPI(epoch uint64, observations []ZPPIObservation, prior ZPPISnapshot, cfg ZPPIBasketConfig) (ZPPISnapshot, error) {
	if epoch == 0 || cfg.MinCoverageBps > BasisPoints || cfg.EWMABps > BasisPoints || (prior.Epoch != 0 && prior.Epoch >= epoch) {
		return ZPPISnapshot{}, ErrZPPI
	}
	var totalWeight uint64
	for component := ZPPIComponent(1); component < ZPPIComponentCount; component++ {
		if cfg.WeightsBps[component] > BasisPoints {
			return ZPPISnapshot{}, ErrZPPI
		}
		totalWeight += uint64(cfg.WeightsBps[component])
		if cfg.WeightsBps[component] > 0 && cfg.ReferencePriceQ9[component] == 0 {
			return ZPPISnapshot{}, ErrZPPI
		}
	}
	if totalWeight == 0 || totalWeight > uint64(BasisPoints) {
		return ZPPISnapshot{}, ErrZPPI
	}

	latest := make(map[ZPPIComponent]ZPPIObservation, len(observations))
	for _, observation := range observations {
		if observation.Component <= ZPPIUnknown || observation.Component >= ZPPIComponentCount || observation.PriceQ9 == 0 {
			return ZPPISnapshot{}, ErrZPPI
		}
		if _, duplicate := latest[observation.Component]; duplicate {
			return ZPPISnapshot{}, ErrZPPI
		}
		latest[observation.Component] = observation
	}

	out := ZPPISnapshot{Epoch: epoch}
	basket := new(big.Int)
	var activeWeight uint64
	for component := ZPPIComponent(1); component < ZPPIComponentCount; component++ {
		weight := uint64(cfg.WeightsBps[component])
		if weight == 0 {
			continue
		}
		observation, ok := latest[component]
		if !ok || !observation.Reliable {
			continue
		}
		index, err := scaledRatio(observation.PriceQ9, ZPPIPriceScaleQ9, cfg.ReferencePriceQ9[component])
		if err != nil {
			return ZPPISnapshot{}, ErrZPPI
		}
		index, err = ewma(prior.ComponentIndexQ9[component], index, cfg.EWMABps)
		if err != nil {
			return ZPPISnapshot{}, ErrZPPI
		}
		out.ComponentIndexQ9[component] = index
		activeWeight += weight
		basket.Add(basket, new(big.Int).Mul(new(big.Int).SetUint64(index), new(big.Int).SetUint64(weight)))
	}
	if activeWeight == 0 {
		return out, nil
	}
	basket.Quo(basket, new(big.Int).SetUint64(activeWeight))
	if !basket.IsUint64() {
		return ZPPISnapshot{}, ErrZPPI
	}
	out.BasketIndexQ9 = basket.Uint64()
	out.CoverageBps = uint32(activeWeight * uint64(BasisPoints) / totalWeight)
	out.Reliable = out.CoverageBps >= cfg.MinCoverageBps
	return out, nil
}

func PurchasingPowerQ9(zppiQ9 uint64) (uint64, error) {
	if zppiQ9 == 0 {
		return 0, ErrZPPI
	}
	return scaledRatio(ZPPIPriceScaleQ9, ZPPIPriceScaleQ9, zppiQ9)
}

// TargetZPPIFromPrior applies the canonical annual target corresponding to a
// 2% purchasing-power decline: 1 / 0.98 ~= 1.020408163.
func TargetZPPIFromPrior(priorZPPIQ9 uint64) (uint64, error) {
	if priorZPPIQ9 == 0 {
		return 0, ErrZPPI
	}
	value := new(big.Int).Mul(new(big.Int).SetUint64(priorZPPIQ9), new(big.Int).SetUint64(TargetZPPIAnnualFactorQ9))
	value.Quo(value, new(big.Int).SetUint64(ZPPIPriceScaleQ9))
	if !value.IsUint64() {
		return 0, ErrZPPI
	}
	return value.Uint64(), nil
}
