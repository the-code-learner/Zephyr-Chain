package economics

import "github.com/zephyr-chain/zephyr-chain/internal/v2/codec"

func ParseEpochAggregate(data []byte) (EpochAggregate, error) {
	r := codec.NewReader(data)
	out := EpochAggregate{}
	var err error
	out.Epoch, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ShardCount, err = r.U32()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ChargedFees, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.BurnedFees, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ValidatorFees, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ReserveFees, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.FinalizedOperations, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ResourceUsed, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ResourceCapacity, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ResourceUtilizationBps, err = r.U32()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.CirculatingNativeSupply, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.AgeWeightedVelocityBps, err = r.U32()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.EscrowBackedComputeDemand, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.VerifiedComputeSupply, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ComputeSupplyReliable, err = r.Bool()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.OpeningComputeBacklog, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ComputeFulfilled, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ComputeExpired, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ComputeBacklog, err = r.U64()
	if err != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ComputeUtilizationBps, err = r.U32()
	if err != nil || r.Done() != nil {
		return EpochAggregate{}, ErrEpochMetrics
	}
	if _, err := out.CanonicalBytes(); err != nil {
		return EpochAggregate{}, err
	}
	if out.ResourceUtilizationBps != ratioBps(out.ResourceUsed, out.ResourceCapacity) {
		return EpochAggregate{}, ErrEpochMetrics
	}
	expectedComputeUtilization := uint32(0)
	if out.VerifiedComputeSupply != 0 {
		expectedComputeUtilization = ratioBps(out.ComputeFulfilled, out.VerifiedComputeSupply)
	}
	if out.ComputeUtilizationBps != expectedComputeUtilization {
		return EpochAggregate{}, ErrEpochMetrics
	}
	return out, nil
}
