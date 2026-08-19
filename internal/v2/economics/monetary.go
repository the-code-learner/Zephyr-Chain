package economics

import (
	"errors"
	"math/big"
)

var ErrMonetaryPolicy = errors.New("invalid Zephyr adaptive monetary policy input")

type MonetaryPolicy struct {
	TargetInflationBps   uint32
	MinInflationBps      uint32
	MaxInflationBps      uint32
	MaxEpochStepBps      uint32
	EpochsPerYear        uint64
	ReserveTargetBps     uint32
	StakeTargetBps       uint32
	UtilizationTargetBps uint32
	VelocityTargetBps    uint32
	OperationsTarget     uint64
	ReserveWeightBps     uint32
	StakeWeightBps       uint32
	UtilizationWeightBps uint32
	VelocityWeightBps    uint32
	OperationsWeightBps  uint32
}

type MonetaryMetrics struct {
	Supply                 uint64
	CirculatingSupply      uint64
	StakedSupply           uint64
	ProtocolReserve        uint64
	BurnedThisEpoch        uint64
	FinalizedOperations    uint64
	ResourceUtilizationBps uint32
	AgeWeightedVelocityBps uint32
	ComputeIndexQ9         uint64
	ComputePriceTrendBps   int32
	ComputeIndexReliable   bool
}

type MonetaryDecision struct {
	Shadow                bool
	TargetInflationBps    uint32
	NetIssuanceTarget     uint64
	BurnOffset            uint64
	GrossMintTarget       uint64
	ProjectedNetChange    uint64
	ReserveRatioBps       uint32
	StakeRatioBps         uint32
	OperationsActivityBps uint32
	ComputeIndexQ9        uint64
	ComputePriceTrendBps  int32
	ComputeIndexReliable  bool
}

func DefaultShadowPolicy() MonetaryPolicy {
	return MonetaryPolicy{
		TargetInflationBps:   200,
		MinInflationBps:      150,
		MaxInflationBps:      250,
		MaxEpochStepBps:      1,
		EpochsPerYear:        365,
		ReserveTargetBps:     1_000,
		StakeTargetBps:       5_000,
		UtilizationTargetBps: 5_000,
		VelocityTargetBps:    5_000,
		OperationsTarget:     1_000_000,
		ReserveWeightBps:     500,
		StakeWeightBps:       500,
		UtilizationWeightBps: 250,
		VelocityWeightBps:    250,
		OperationsWeightBps:  100,
	}
}

// EvaluateShadow computes the monetary action Zephyr would take for an epoch,
// but deliberately does not mutate supply. This is the only supported v2 mode
// until devnet simulations establish stability and manipulation resistance.
func EvaluateShadow(priorTargetBps uint32, metrics MonetaryMetrics, policy MonetaryPolicy) (MonetaryDecision, error) {
	if err := validateMonetary(metrics, policy); err != nil {
		return MonetaryDecision{}, err
	}
	reserveRatio := ratioBps(metrics.ProtocolReserve, metrics.Supply)
	stakeRatio := ratioBps(metrics.StakedSupply, metrics.CirculatingSupply)
	operationsActivity := activityBps(metrics.FinalizedOperations, policy.OperationsTarget)

	correction := int64(0)
	correction += weightedGap(policy.ReserveTargetBps, reserveRatio, policy.ReserveWeightBps)
	correction += weightedGap(policy.StakeTargetBps, stakeRatio, policy.StakeWeightBps)
	correction += weightedGap(policy.UtilizationTargetBps, metrics.ResourceUtilizationBps, policy.UtilizationWeightBps)
	correction += weightedGap(policy.VelocityTargetBps, metrics.AgeWeightedVelocityBps, policy.VelocityWeightBps)
	correction += weightedGap(BasisPoints, operationsActivity, policy.OperationsWeightBps)

	target := clampTarget(int64(policy.TargetInflationBps)+correction, policy.MinInflationBps, policy.MaxInflationBps)
	if priorTargetBps != 0 {
		target = rateLimitTarget(priorTargetBps, target, policy.MaxEpochStepBps)
	}
	netTarget, err := epochIssuance(metrics.Supply, target, policy.EpochsPerYear)
	if err != nil {
		return MonetaryDecision{}, err
	}
	gross, err := addUint64(netTarget, metrics.BurnedThisEpoch)
	if err != nil {
		return MonetaryDecision{}, err
	}
	return MonetaryDecision{
		Shadow:                true,
		TargetInflationBps:    target,
		NetIssuanceTarget:     netTarget,
		BurnOffset:            metrics.BurnedThisEpoch,
		GrossMintTarget:       gross,
		ProjectedNetChange:    netTarget,
		ReserveRatioBps:       reserveRatio,
		StakeRatioBps:         stakeRatio,
		OperationsActivityBps: operationsActivity,
		ComputeIndexQ9:        metrics.ComputeIndexQ9,
		ComputePriceTrendBps:  metrics.ComputePriceTrendBps,
		ComputeIndexReliable:  metrics.ComputeIndexReliable,
	}, nil
}

func validateMonetary(metrics MonetaryMetrics, policy MonetaryPolicy) error {
	if metrics.Supply == 0 || metrics.CirculatingSupply == 0 || metrics.CirculatingSupply > metrics.Supply ||
		metrics.StakedSupply > metrics.CirculatingSupply || metrics.ProtocolReserve > metrics.Supply ||
		policy.EpochsPerYear == 0 || policy.OperationsTarget == 0 || policy.MinInflationBps > policy.TargetInflationBps ||
		policy.TargetInflationBps > policy.MaxInflationBps || policy.MaxInflationBps > BasisPoints ||
		policy.ReserveTargetBps > BasisPoints || policy.StakeTargetBps > BasisPoints ||
		policy.UtilizationTargetBps > BasisPoints || policy.VelocityTargetBps > BasisPoints ||
		metrics.ResourceUtilizationBps > 2*BasisPoints || metrics.AgeWeightedVelocityBps > 2*BasisPoints {
		return ErrMonetaryPolicy
	}
	weights := []uint32{policy.ReserveWeightBps, policy.StakeWeightBps, policy.UtilizationWeightBps, policy.VelocityWeightBps, policy.OperationsWeightBps}
	for _, weight := range weights {
		if weight > BasisPoints {
			return ErrMonetaryPolicy
		}
	}
	return nil
}

func weightedGap(target, actual, weight uint32) int64 {
	gap := int64(target) - int64(actual)
	return gap * int64(weight) / int64(BasisPoints)
}

func clampTarget(target int64, minimum, maximum uint32) uint32 {
	if target < int64(minimum) {
		return minimum
	}
	if target > int64(maximum) {
		return maximum
	}
	return uint32(target)
}

func rateLimitTarget(prior, next, maximumStep uint32) uint32 {
	if maximumStep == 0 || prior == next {
		return prior
	}
	if next > prior {
		if next-prior > maximumStep {
			return prior + maximumStep
		}
		return next
	}
	if prior-next > maximumStep {
		return prior - maximumStep
	}
	return next
}

func ratioBps(value, total uint64) uint32 {
	if total == 0 {
		return 0
	}
	ratio := new(big.Int).Mul(new(big.Int).SetUint64(value), new(big.Int).SetUint64(uint64(BasisPoints)))
	ratio.Quo(ratio, new(big.Int).SetUint64(total))
	limit := new(big.Int).SetUint64(uint64(BasisPoints))
	if ratio.Cmp(limit) > 0 {
		return BasisPoints
	}
	return uint32(ratio.Uint64())
}

func activityBps(value, target uint64) uint32 {
	if target == 0 {
		return 0
	}
	ratio := new(big.Int).Mul(new(big.Int).SetUint64(value), new(big.Int).SetUint64(uint64(BasisPoints)))
	ratio.Quo(ratio, new(big.Int).SetUint64(target))
	limit := new(big.Int).SetUint64(uint64(2 * BasisPoints))
	if ratio.Cmp(limit) > 0 {
		return 2 * BasisPoints
	}
	return uint32(ratio.Uint64())
}

func epochIssuance(supply uint64, targetBps uint32, epochsPerYear uint64) (uint64, error) {
	value := new(big.Int).Mul(new(big.Int).SetUint64(supply), new(big.Int).SetUint64(uint64(targetBps)))
	value.Quo(value, new(big.Int).SetUint64(uint64(BasisPoints)))
	value.Quo(value, new(big.Int).SetUint64(epochsPerYear))
	if !value.IsUint64() {
		return 0, ErrMonetaryPolicy
	}
	return value.Uint64(), nil
}

func addUint64(a, b uint64) (uint64, error) {
	value := new(big.Int).Add(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
	if !value.IsUint64() {
		return 0, ErrMonetaryPolicy
	}
	return value.Uint64(), nil
}
