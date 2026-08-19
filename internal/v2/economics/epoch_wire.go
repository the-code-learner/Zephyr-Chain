package economics

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func (a EpochAggregate) CanonicalBytes() ([]byte, error) {
	if a.Epoch == 0 || a.ShardCount == 0 || a.ResourceCapacity == 0 || a.ResourceUsed > a.ResourceCapacity ||
		a.ResourceUtilizationBps > BasisPoints || a.ComputeUtilizationBps > BasisPoints || a.AgeWeightedVelocityBps > 10*BasisPoints ||
		(a.ComputeSupplyReliable && a.ComputeFulfilled > a.VerifiedComputeSupply) || !validComputeFlow(
		a.OpeningComputeBacklog,
		a.EscrowBackedComputeDemand,
		a.ComputeFulfilled,
		a.ComputeExpired,
		a.ComputeBacklog,
	) {
		return nil, ErrEpochMetrics
	}
	if a.BurnedFees > a.ChargedFees || a.ValidatorFees > a.ChargedFees-a.BurnedFees ||
		a.ReserveFees != a.ChargedFees-a.BurnedFees-a.ValidatorFees {
		return nil, ErrEpochMetrics
	}
	var w codec.Writer
	w.U64(a.Epoch)
	w.U32(a.ShardCount)
	w.U64(a.ChargedFees)
	w.U64(a.BurnedFees)
	w.U64(a.ValidatorFees)
	w.U64(a.ReserveFees)
	w.U64(a.FinalizedOperations)
	w.U64(a.ResourceUsed)
	w.U64(a.ResourceCapacity)
	w.U32(a.ResourceUtilizationBps)
	w.U64(a.CirculatingNativeSupply)
	w.U32(a.AgeWeightedVelocityBps)
	w.U64(a.EscrowBackedComputeDemand)
	w.U64(a.VerifiedComputeSupply)
	w.Bool(a.ComputeSupplyReliable)
	w.U64(a.OpeningComputeBacklog)
	w.U64(a.ComputeFulfilled)
	w.U64(a.ComputeExpired)
	w.U64(a.ComputeBacklog)
	w.U32(a.ComputeUtilizationBps)
	return w.BytesCopy(), nil
}

func (a EpochAggregate) Hash() (types.Hash, error) {
	raw, err := a.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/epoch-economics/v2", raw)), nil
}
