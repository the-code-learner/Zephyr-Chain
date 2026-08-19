package economics

import (
	"errors"
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
)

var ErrVelocity = errors.New("invalid Zephyr age-weighted velocity input")

type VelocityPolicy struct {
	MinAgeBlocks        uint64
	FullWeightAgeBlocks uint64
	MaxVelocityBps      uint32
}

type VelocitySnapshot struct {
	AgeWeightedVelocityBps uint32
	ObservedSpends         uint64
	EligibleSpends         uint64
	UnknownAgeSpends       uint64
	FreshSpends            uint64
}

type VelocityAccumulator struct {
	policy             VelocityPolicy
	weightedValueBps   big.Int
	observedSpends     uint64
	eligibleSpends     uint64
	unknownAgeSpends   uint64
	freshSpends        uint64
}

func NewVelocityAccumulator(policy VelocityPolicy) (*VelocityAccumulator, error) {
	if policy.FullWeightAgeBlocks == 0 || policy.MinAgeBlocks > policy.FullWeightAgeBlocks || policy.MaxVelocityBps == 0 || policy.MaxVelocityBps > 10*BasisPoints {
		return nil, ErrVelocity
	}
	return &VelocityAccumulator{policy: policy}, nil
}

// ObserveCoin records a finalized spend. CreatedHeight must come from the
// consensus-stamped coin object, never from an untrusted RPC timestamp.
func (a *VelocityAccumulator) ObserveCoin(coin object.Coin, spendHeight uint64) error {
	if a == nil || coin.Amount == 0 {
		return ErrVelocity
	}
	a.observedSpends++
	if coin.CreatedHeight == 0 {
		a.unknownAgeSpends++
		return nil
	}
	if spendHeight <= coin.CreatedHeight {
		return ErrVelocity
	}
	age := spendHeight - coin.CreatedHeight
	if age < a.policy.MinAgeBlocks {
		a.freshSpends++
		return nil
	}
	if age > a.policy.FullWeightAgeBlocks {
		age = a.policy.FullWeightAgeBlocks
	}
	weight := new(big.Int).Mul(new(big.Int).SetUint64(age), new(big.Int).SetUint64(uint64(BasisPoints)))
	weight.Quo(weight, new(big.Int).SetUint64(a.policy.FullWeightAgeBlocks))
	contribution := new(big.Int).Mul(new(big.Int).SetUint64(coin.Amount), weight)
	a.weightedValueBps.Add(&a.weightedValueBps, contribution)
	a.eligibleSpends++
	return nil
}

// Finalize normalizes the accumulated age-weighted moved value by circulating
// supply. A full-age spend of 50% of circulating supply contributes 5,000 bps.
func (a *VelocityAccumulator) Finalize(circulatingSupply uint64) (VelocitySnapshot, error) {
	if a == nil || circulatingSupply == 0 {
		return VelocitySnapshot{}, ErrVelocity
	}
	value := new(big.Int).Quo(new(big.Int).Set(&a.weightedValueBps), new(big.Int).SetUint64(circulatingSupply))
	maximum := new(big.Int).SetUint64(uint64(a.policy.MaxVelocityBps))
	if value.Cmp(maximum) > 0 {
		value.Set(maximum)
	}
	return VelocitySnapshot{
		AgeWeightedVelocityBps: uint32(value.Uint64()),
		ObservedSpends:         a.observedSpends,
		EligibleSpends:         a.eligibleSpends,
		UnknownAgeSpends:       a.unknownAgeSpends,
		FreshSpends:            a.freshSpends,
	}, nil
}
