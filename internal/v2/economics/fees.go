package economics

import (
	"errors"
	"math/big"
)

var (
	ErrFeePolicy   = errors.New("invalid Zephyr fee policy")
	ErrResourceFee = errors.New("invalid Zephyr resource fee input")
)

type FeePolicy struct {
	BurnBps      uint32
	ValidatorBps uint32
	ReserveBps   uint32
}

type FeeAllocation struct {
	Total      uint64
	Burn       uint64
	Validators uint64
	Reserve    uint64
}

// CompatibilityFeePolicy preserves the current v2 executor behavior while fee
// accounting is moved into authenticated monetary state: the full signed Fee is
// counted as burn. Alternative splits can be simulated without activating them.
func CompatibilityFeePolicy() FeePolicy {
	return FeePolicy{BurnBps: BasisPoints}
}

func (p FeePolicy) Validate() error {
	total := uint64(p.BurnBps) + uint64(p.ValidatorBps) + uint64(p.ReserveBps)
	if p.BurnBps > BasisPoints || p.ValidatorBps > BasisPoints || p.ReserveBps > BasisPoints || total != uint64(BasisPoints) {
		return ErrFeePolicy
	}
	return nil
}

// SplitFee uses floor division for validator and reserve shares. Any indivisible
// remainder is assigned to burn, making conservation exact and deterministic.
func SplitFee(fee uint64, policy FeePolicy) (FeeAllocation, error) {
	if err := policy.Validate(); err != nil {
		return FeeAllocation{}, err
	}
	validators, err := shareBps(fee, policy.ValidatorBps)
	if err != nil {
		return FeeAllocation{}, err
	}
	reserve, err := shareBps(fee, policy.ReserveBps)
	if err != nil || validators > fee || reserve > fee-validators {
		return FeeAllocation{}, ErrFeePolicy
	}
	burn := fee - validators - reserve
	return FeeAllocation{Total: fee, Burn: burn, Validators: validators, Reserve: reserve}, nil
}

type ResourceUsage struct {
	BaseTransactions      uint64
	SignatureOps          uint64
	WitnessBytes          uint64
	StateReads            uint64
	StateWrites           uint64
	ContractFuel          uint64
	DataAvailabilityBytes uint64
	CrossShardReceipts    uint64
}

type ResourcePrices struct {
	BaseTransaction      uint64
	SignatureOp          uint64
	WitnessByte          uint64
	StateRead            uint64
	StateWrite           uint64
	ContractFuel         uint64
	DataAvailabilityByte uint64
	CrossShardReceipt    uint64
}

func QuoteResourceFee(usage ResourceUsage, prices ResourcePrices) (uint64, error) {
	pairs := [][2]uint64{
		{usage.BaseTransactions, prices.BaseTransaction},
		{usage.SignatureOps, prices.SignatureOp},
		{usage.WitnessBytes, prices.WitnessByte},
		{usage.StateReads, prices.StateRead},
		{usage.StateWrites, prices.StateWrite},
		{usage.ContractFuel, prices.ContractFuel},
		{usage.DataAvailabilityBytes, prices.DataAvailabilityByte},
		{usage.CrossShardReceipts, prices.CrossShardReceipt},
	}
	total := new(big.Int)
	for _, pair := range pairs {
		term := new(big.Int).Mul(new(big.Int).SetUint64(pair[0]), new(big.Int).SetUint64(pair[1]))
		total.Add(total, term)
	}
	if !total.IsUint64() {
		return 0, ErrResourceFee
	}
	return total.Uint64(), nil
}

type BaseFeePolicy struct {
	TargetUsage           uint64
	AdjustmentDenominator uint64
	MinBaseFee            uint64
	MaxBaseFee            uint64
}

func (p BaseFeePolicy) Validate() error {
	if p.TargetUsage == 0 || p.AdjustmentDenominator == 0 || p.MinBaseFee == 0 || p.MaxBaseFee < p.MinBaseFee {
		return ErrFeePolicy
	}
	return nil
}

// NextBaseFee is a bounded integer-only congestion controller. It is provided
// for simulation/reference use and is not activated as the live v2 fee market.
func NextBaseFee(current, used uint64, policy BaseFeePolicy) (uint64, error) {
	if err := policy.Validate(); err != nil || current < policy.MinBaseFee || current > policy.MaxBaseFee {
		return 0, ErrFeePolicy
	}
	if used == policy.TargetUsage {
		return current, nil
	}
	var distance uint64
	increase := used > policy.TargetUsage
	if increase {
		distance = used - policy.TargetUsage
	} else {
		distance = policy.TargetUsage - used
	}
	change := new(big.Int).Mul(new(big.Int).SetUint64(current), new(big.Int).SetUint64(distance))
	change.Quo(change, new(big.Int).SetUint64(policy.TargetUsage))
	change.Quo(change, new(big.Int).SetUint64(policy.AdjustmentDenominator))
	if change.Sign() == 0 {
		change.SetUint64(1)
	}
	if !change.IsUint64() {
		return 0, ErrResourceFee
	}
	delta := change.Uint64()
	if increase {
		next := new(big.Int).Add(new(big.Int).SetUint64(current), new(big.Int).SetUint64(delta))
		max := new(big.Int).SetUint64(policy.MaxBaseFee)
		if next.Cmp(max) > 0 {
			return policy.MaxBaseFee, nil
		}
		return next.Uint64(), nil
	}
	if delta >= current || current-delta < policy.MinBaseFee {
		return policy.MinBaseFee, nil
	}
	return current - delta, nil
}

func shareBps(value uint64, bps uint32) (uint64, error) {
	share := new(big.Int).Mul(new(big.Int).SetUint64(value), new(big.Int).SetUint64(uint64(bps)))
	share.Quo(share, new(big.Int).SetUint64(uint64(BasisPoints)))
	if !share.IsUint64() {
		return 0, ErrFeePolicy
	}
	return share.Uint64(), nil
}
