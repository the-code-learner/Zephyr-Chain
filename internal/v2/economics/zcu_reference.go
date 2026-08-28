package economics

import (
	"errors"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
)

const ZCUReferenceScaleQ9 uint64 = 1_000_000_000

var ErrZCUReference = errors.New("invalid Zephyr compute unit reference input")

// VerifiedComputeSlot is an economically weighted observation of concurrently
// usable compute capacity. PerformanceQ9 must come from an authenticated
// benchmark result; provider-declared peak performance is intentionally absent.
type VerifiedComputeSlot struct {
	Class               compute.WorkClass
	PerformanceQ9       uint64
	DeliveredSlotTime   uint64
	AvailabilityEWMABps uint32
	SuccessEWMABps      uint32
	ConfidenceBps       uint32
}

type ZCUReferenceConfig struct {
	MinSlotsPerClass  uint32
	EWMABps           uint32
	MaxEpochChangeBps uint32
}

type ZCUReferenceSnapshot struct {
	Epoch            uint64
	ClassReferenceQ9 [compute.WorkClassCount]uint64
	ClassSlots       [compute.WorkClassCount]uint32
	ClassWeight      [compute.WorkClassCount]uint64
	ClassReliable    [compute.WorkClassCount]bool
}

type weightedSlot struct {
	performance uint64
	weight      uint64
}

func BuildZCUReference(epoch uint64, slots []VerifiedComputeSlot, prior ZCUReferenceSnapshot, cfg ZCUReferenceConfig) (ZCUReferenceSnapshot, error) {
	if epoch == 0 || cfg.MinSlotsPerClass == 0 || cfg.EWMABps > BasisPoints || cfg.MaxEpochChangeBps > BasisPoints {
		return ZCUReferenceSnapshot{}, ErrZCUReference
	}
	if prior.Epoch != 0 && prior.Epoch >= epoch {
		return ZCUReferenceSnapshot{}, ErrZCUReference
	}

	byClass := make([][]weightedSlot, int(compute.WorkClassCount))
	out := ZCUReferenceSnapshot{Epoch: epoch}
	for _, slot := range slots {
		if slot.Class <= compute.WorkUnknown || slot.Class >= compute.WorkClassCount || slot.PerformanceQ9 == 0 || slot.DeliveredSlotTime == 0 ||
			slot.AvailabilityEWMABps > BasisPoints || slot.SuccessEWMABps > BasisPoints || slot.ConfidenceBps > BasisPoints {
			return ZCUReferenceSnapshot{}, ErrZCUReference
		}
		weight, err := effectiveSlotWeight(slot)
		if err != nil {
			return ZCUReferenceSnapshot{}, err
		}
		if weight == 0 {
			continue
		}
		byClass[slot.Class] = append(byClass[slot.Class], weightedSlot{performance: slot.PerformanceQ9, weight: weight})
	}

	for class := compute.WorkClass(1); class < compute.WorkClassCount; class++ {
		classSlots := byClass[class]
		out.ClassSlots[class] = uint32(len(classSlots))
		if len(classSlots) == 0 {
			continue
		}
		sort.Slice(classSlots, func(i, j int) bool {
			if classSlots[i].performance == classSlots[j].performance {
				return classSlots[i].weight < classSlots[j].weight
			}
			return classSlots[i].performance < classSlots[j].performance
		})
		median, totalWeight, err := weightedMedianSlot(classSlots)
		if err != nil {
			return ZCUReferenceSnapshot{}, err
		}
		out.ClassWeight[class] = totalWeight
		if out.ClassSlots[class] < cfg.MinSlotsPerClass {
			continue
		}
		reference, err := ewma(prior.ClassReferenceQ9[class], median, cfg.EWMABps)
		if err != nil {
			return ZCUReferenceSnapshot{}, err
		}
		reference, err = rateLimitQ9(prior.ClassReferenceQ9[class], reference, cfg.MaxEpochChangeBps)
		if err != nil {
			return ZCUReferenceSnapshot{}, err
		}
		out.ClassReferenceQ9[class] = reference
		out.ClassReliable[class] = reference != 0
	}
	return out, nil
}

func effectiveSlotWeight(slot VerifiedComputeSlot) (uint64, error) {
	value := new(big.Int).SetUint64(slot.DeliveredSlotTime)
	for _, factor := range []uint32{slot.AvailabilityEWMABps, slot.SuccessEWMABps, slot.ConfidenceBps} {
		value.Mul(value, new(big.Int).SetUint64(uint64(factor)))
		value.Quo(value, new(big.Int).SetUint64(uint64(BasisPoints)))
	}
	if !value.IsUint64() {
		return 0, ErrZCUReference
	}
	return value.Uint64(), nil
}

func weightedMedianSlot(slots []weightedSlot) (uint64, uint64, error) {
	if len(slots) == 0 {
		return 0, 0, ErrZCUReference
	}
	total := new(big.Int)
	for _, slot := range slots {
		if slot.performance == 0 || slot.weight == 0 {
			return 0, 0, ErrZCUReference
		}
		total.Add(total, new(big.Int).SetUint64(slot.weight))
	}
	if !total.IsUint64() || total.Sign() == 0 {
		return 0, 0, ErrZCUReference
	}
	totalWeight := total.Uint64()
	threshold := totalWeight/2 + totalWeight%2
	var cumulative uint64
	for _, slot := range slots {
		if ^uint64(0)-cumulative < slot.weight {
			return 0, 0, ErrZCUReference
		}
		cumulative += slot.weight
		if cumulative >= threshold {
			return slot.performance, totalWeight, nil
		}
	}
	return 0, 0, ErrZCUReference
}

func rateLimitQ9(prior, next uint64, maxChangeBps uint32) (uint64, error) {
	if prior == 0 || maxChangeBps == 0 || prior == next {
		return next, nil
	}
	upper := new(big.Int).Mul(new(big.Int).SetUint64(prior), new(big.Int).SetUint64(uint64(BasisPoints+maxChangeBps)))
	upper.Quo(upper, new(big.Int).SetUint64(uint64(BasisPoints)))
	lower := new(big.Int).Mul(new(big.Int).SetUint64(prior), new(big.Int).SetUint64(uint64(BasisPoints-maxChangeBps)))
	lower.Quo(lower, new(big.Int).SetUint64(uint64(BasisPoints)))
	if !upper.IsUint64() || !lower.IsUint64() {
		return 0, ErrZCUReference
	}
	if next > upper.Uint64() {
		return upper.Uint64(), nil
	}
	if next < lower.Uint64() {
		return lower.Uint64(), nil
	}
	return next, nil
}
