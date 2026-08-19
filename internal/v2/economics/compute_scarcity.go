package economics

import (
	"errors"
	"math/big"
)

var ErrComputeScarcity = errors.New("invalid Zephyr compute scarcity input")

type ComputeScarcityConfig struct {
	DemandSupplyWeightBps uint32
	PriceTrendWeightBps   uint32
	BacklogWeightBps      uint32
	UtilizationWeightBps  uint32
	FulfillmentWeightBps  uint32
	UtilizationTargetBps  uint32
	MinDemandUnits        uint64
	MinSupplyUnits        uint64
	MaxAbsScoreBps        uint32
}

type ComputeMarketMetrics struct {
	EscrowBackedDemandUnits uint64
	VerifiedSupplyUnits     uint64
	BacklogUnits            uint64
	FulfilledUnits          uint64
	UtilizationBps          uint32
	ComputePriceTrendBps    int32
	ComputeIndexReliable    bool
}

type ComputeScarcitySnapshot struct {
	Epoch                   uint64
	DemandSupplyPressureBps int32
	PriceTrendPressureBps   int32
	BacklogPressureBps      int32
	UtilizationPressureBps  int32
	FulfillmentPressureBps  int32
	ScoreBps                int32
	Reliable                bool
}

func DefaultComputeScarcityConfig() ComputeScarcityConfig {
	return ComputeScarcityConfig{
		DemandSupplyWeightBps: 3_000,
		PriceTrendWeightBps:   2_000,
		BacklogWeightBps:      2_000,
		UtilizationWeightBps:  1_500,
		FulfillmentWeightBps:  1_500,
		UtilizationTargetBps:  7_000,
		MinDemandUnits:        1_000,
		MinSupplyUnits:        1_000,
		MaxAbsScoreBps:        10_000,
	}
}

// BuildComputeScarcity calculates the Zephyr Compute Scarcity Index (ZCSI).
// Demand must represent standardized, escrow-backed work. Supply must represent
// standardized, benchmarked and collateralized capacity. Advertised prices or
// self-reported peak FLOPS are not valid inputs.
func BuildComputeScarcity(epoch uint64, metrics ComputeMarketMetrics, cfg ComputeScarcityConfig) (ComputeScarcitySnapshot, error) {
	if epoch == 0 || cfg.UtilizationTargetBps > BasisPoints || cfg.MaxAbsScoreBps == 0 || cfg.MaxAbsScoreBps > BasisPoints ||
		metrics.UtilizationBps > BasisPoints || metrics.BacklogUnits > metrics.EscrowBackedDemandUnits ||
		metrics.FulfilledUnits > metrics.EscrowBackedDemandUnits {
		return ComputeScarcitySnapshot{}, ErrComputeScarcity
	}
	weights := []uint32{
		cfg.DemandSupplyWeightBps,
		cfg.PriceTrendWeightBps,
		cfg.BacklogWeightBps,
		cfg.UtilizationWeightBps,
		cfg.FulfillmentWeightBps,
	}
	var totalWeight uint64
	for _, weight := range weights {
		if weight > BasisPoints {
			return ComputeScarcitySnapshot{}, ErrComputeScarcity
		}
		totalWeight += uint64(weight)
	}
	if totalWeight == 0 {
		return ComputeScarcitySnapshot{}, ErrComputeScarcity
	}

	out := ComputeScarcitySnapshot{Epoch: epoch}
	out.DemandSupplyPressureBps = signedRatioPressure(metrics.EscrowBackedDemandUnits, metrics.VerifiedSupplyUnits)
	out.BacklogPressureBps = unsignedRatioPressure(metrics.BacklogUnits, metrics.EscrowBackedDemandUnits)
	out.UtilizationPressureBps = clampSignedBps(int64(metrics.UtilizationBps) - int64(cfg.UtilizationTargetBps))
	fulfilledBps := unsignedRatioPressure(metrics.FulfilledUnits, metrics.EscrowBackedDemandUnits)
	out.FulfillmentPressureBps = int32(BasisPoints) - fulfilledBps
	if metrics.EscrowBackedDemandUnits == 0 {
		out.FulfillmentPressureBps = 0
	}
	if metrics.ComputeIndexReliable {
		out.PriceTrendPressureBps = clampSignedBps(int64(metrics.ComputePriceTrendBps))
	}

	weighted := int64(out.DemandSupplyPressureBps)*int64(cfg.DemandSupplyWeightBps) +
		int64(out.BacklogPressureBps)*int64(cfg.BacklogWeightBps) +
		int64(out.UtilizationPressureBps)*int64(cfg.UtilizationWeightBps) +
		int64(out.FulfillmentPressureBps)*int64(cfg.FulfillmentWeightBps)
	effectiveWeight := totalWeight
	if metrics.ComputeIndexReliable {
		weighted += int64(out.PriceTrendPressureBps) * int64(cfg.PriceTrendWeightBps)
	} else {
		effectiveWeight -= uint64(cfg.PriceTrendWeightBps)
	}
	if effectiveWeight == 0 {
		return ComputeScarcitySnapshot{}, ErrComputeScarcity
	}
	out.ScoreBps = clampSigned(int64(weighted)/int64(effectiveWeight), int32(cfg.MaxAbsScoreBps))
	out.Reliable = metrics.EscrowBackedDemandUnits >= cfg.MinDemandUnits && metrics.VerifiedSupplyUnits >= cfg.MinSupplyUnits
	return out, nil
}

func signedRatioPressure(demand, supply uint64) int32 {
	if demand == 0 && supply == 0 {
		return 0
	}
	if supply == 0 {
		return int32(BasisPoints)
	}
	delta := new(big.Int).Sub(new(big.Int).SetUint64(demand), new(big.Int).SetUint64(supply))
	delta.Mul(delta, new(big.Int).SetUint64(uint64(BasisPoints)))
	delta.Quo(delta, new(big.Int).SetUint64(supply))
	if delta.Cmp(big.NewInt(int64(BasisPoints))) > 0 {
		return int32(BasisPoints)
	}
	if delta.Cmp(big.NewInt(-int64(BasisPoints))) < 0 {
		return -int32(BasisPoints)
	}
	return int32(delta.Int64())
}

func unsignedRatioPressure(value, total uint64) int32 {
	if total == 0 {
		return 0
	}
	ratio := new(big.Int).Mul(new(big.Int).SetUint64(value), new(big.Int).SetUint64(uint64(BasisPoints)))
	ratio.Quo(ratio, new(big.Int).SetUint64(total))
	if ratio.Cmp(new(big.Int).SetUint64(uint64(BasisPoints))) > 0 {
		return int32(BasisPoints)
	}
	return int32(ratio.Int64())
}

func clampSignedBps(value int64) int32 {
	return clampSigned(value, int32(BasisPoints))
}

func clampSigned(value int64, maximum int32) int32 {
	if value > int64(maximum) {
		return maximum
	}
	if value < -int64(maximum) {
		return -maximum
	}
	return int32(value)
}
