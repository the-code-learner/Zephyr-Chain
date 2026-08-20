package economics

import (
	"errors"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
)

const (
	BasisPoints  uint32 = 10_000
	PriceScaleQ9 uint64 = 1_000_000_000
)

var ErrComputeIndex = errors.New("invalid Zephyr compute price index input")

type ComputeIndexConfig struct {
	WeightsBps         [compute.WorkClassCount]uint32
	MinSamplesPerClass uint32
	MinCoverageBps     uint32
	EWMABps            uint32
}

type ComputeIndexSnapshot struct {
	Epoch         uint64
	ClassPriceQ9  [compute.WorkClassCount]uint64
	ClassSamples  [compute.WorkClassCount]uint64
	BasketPriceQ9 uint64
	CoverageBps   uint32
	Reliable      bool
	TotalSamples  uint64
}

func BuildComputeIndex(epoch uint64, observations []compute.VerifiedWork, prior ComputeIndexSnapshot, cfg ComputeIndexConfig) (ComputeIndexSnapshot, error) {
	if epoch == 0 || cfg.MinSamplesPerClass == 0 || cfg.MinCoverageBps > BasisPoints || cfg.EWMABps > BasisPoints {
		return ComputeIndexSnapshot{}, ErrComputeIndex
	}
	var totalWeight uint64
	for class := compute.WorkClass(1); class < compute.WorkClassCount; class++ {
		totalWeight += uint64(cfg.WeightsBps[class])
	}
	if totalWeight == 0 {
		return ComputeIndexSnapshot{}, ErrComputeIndex
	}

	prices := make([][]uint64, int(compute.WorkClassCount))
	for _, observation := range observations {
		if observation.Class <= compute.WorkUnknown || observation.Class >= compute.WorkClassCount || observation.Units == 0 || observation.PaidZPH == 0 {
			return ComputeIndexSnapshot{}, ErrComputeIndex
		}
		price, err := scaledRatio(observation.PaidZPH, PriceScaleQ9, observation.Units)
		if err != nil {
			return ComputeIndexSnapshot{}, err
		}
		prices[observation.Class] = append(prices[observation.Class], price)
	}

	out := ComputeIndexSnapshot{Epoch: epoch}
	basket := new(big.Int)
	var activeWeight uint64
	for class := compute.WorkClass(1); class < compute.WorkClassCount; class++ {
		classPrices := prices[class]
		out.ClassSamples[class] = uint64(len(classPrices))
		out.TotalSamples += uint64(len(classPrices))
		if uint32(len(classPrices)) < cfg.MinSamplesPerClass || cfg.WeightsBps[class] == 0 {
			continue
		}
		sort.Slice(classPrices, func(i, j int) bool { return classPrices[i] < classPrices[j] })
		median := medianUint64(classPrices)
		current, err := ewma(prior.ClassPriceQ9[class], median, cfg.EWMABps)
		if err != nil {
			return ComputeIndexSnapshot{}, err
		}
		out.ClassPriceQ9[class] = current
		weight := uint64(cfg.WeightsBps[class])
		activeWeight += weight
		term := new(big.Int).Mul(new(big.Int).SetUint64(current), new(big.Int).SetUint64(weight))
		basket.Add(basket, term)
	}
	if activeWeight == 0 {
		return out, nil
	}
	basket.Div(basket, new(big.Int).SetUint64(activeWeight))
	if !basket.IsUint64() {
		return ComputeIndexSnapshot{}, ErrComputeIndex
	}
	out.BasketPriceQ9 = basket.Uint64()
	coverage := activeWeight * uint64(BasisPoints) / totalWeight
	if coverage > uint64(BasisPoints) {
		coverage = uint64(BasisPoints)
	}
	out.CoverageBps = uint32(coverage)
	out.Reliable = out.CoverageBps >= cfg.MinCoverageBps
	return out, nil
}

func ComputePriceTrendBps(current, prior uint64) int32 {
	if current == 0 || prior == 0 || current == prior {
		return 0
	}
	delta := new(big.Int).Sub(new(big.Int).SetUint64(current), new(big.Int).SetUint64(prior))
	delta.Mul(delta, new(big.Int).SetUint64(uint64(BasisPoints)))
	delta.Quo(delta, new(big.Int).SetUint64(prior))
	limit := big.NewInt(int64(BasisPoints))
	negativeLimit := new(big.Int).Neg(new(big.Int).Set(limit))
	if delta.Cmp(limit) > 0 {
		return int32(BasisPoints)
	}
	if delta.Cmp(negativeLimit) < 0 {
		return -int32(BasisPoints)
	}
	return int32(delta.Int64())
}

func scaledRatio(value, scale, divisor uint64) (uint64, error) {
	if divisor == 0 {
		return 0, ErrComputeIndex
	}
	numerator := new(big.Int).Mul(new(big.Int).SetUint64(value), new(big.Int).SetUint64(scale))
	numerator.Quo(numerator, new(big.Int).SetUint64(divisor))
	if !numerator.IsUint64() {
		return 0, ErrComputeIndex
	}
	return numerator.Uint64(), nil
}

func medianUint64(values []uint64) uint64 {
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	low := values[middle-1]
	high := values[middle]
	return low + (high-low)/2
}

func ewma(prior, current uint64, alphaBps uint32) (uint64, error) {
	if prior == 0 || alphaBps == BasisPoints {
		return current, nil
	}
	if alphaBps == 0 {
		return prior, nil
	}
	left := new(big.Int).Mul(new(big.Int).SetUint64(prior), new(big.Int).SetUint64(uint64(BasisPoints-alphaBps)))
	right := new(big.Int).Mul(new(big.Int).SetUint64(current), new(big.Int).SetUint64(uint64(alphaBps)))
	left.Add(left, right)
	left.Quo(left, new(big.Int).SetUint64(uint64(BasisPoints)))
	if !left.IsUint64() {
		return 0, ErrComputeIndex
	}
	return left.Uint64(), nil
}
