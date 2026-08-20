package economics

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const shadowEpochEngineCheckpointVersion uint16 = 1

func ShadowEpochEngineConfigHash(config ShadowEpochEngineConfig) types.Hash {
	var w codec.Writer
	for class := compute.WorkClass(0); class < compute.WorkClassCount; class++ {
		w.U32(config.ComputeIndex.WeightsBps[class])
	}
	w.U32(config.ComputeIndex.MinSamplesPerClass)
	w.U32(config.ComputeIndex.MinCoverageBps)
	w.U32(config.ComputeIndex.EWMABps)

	w.U32(config.ComputeScarcity.DemandSupplyWeightBps)
	w.U32(config.ComputeScarcity.PriceTrendWeightBps)
	w.U32(config.ComputeScarcity.BacklogWeightBps)
	w.U32(config.ComputeScarcity.UtilizationWeightBps)
	w.U32(config.ComputeScarcity.FulfillmentWeightBps)
	w.U32(config.ComputeScarcity.UtilizationTargetBps)
	w.U64(config.ComputeScarcity.MinDemandUnits)
	w.U64(config.ComputeScarcity.MinSupplyUnits)
	w.U32(config.ComputeScarcity.MaxAbsScoreBps)

	w.U32(config.Monetary.TargetInflationBps)
	w.U32(config.Monetary.MinInflationBps)
	w.U32(config.Monetary.MaxInflationBps)
	w.U32(config.Monetary.MaxEpochStepBps)
	w.U64(config.Monetary.EpochsPerYear)
	w.U32(config.Monetary.ReserveTargetBps)
	w.U32(config.Monetary.StakeTargetBps)
	w.U32(config.Monetary.UtilizationTargetBps)
	w.U32(config.Monetary.VelocityTargetBps)
	w.U64(config.Monetary.OperationsTarget)
	w.U32(config.Monetary.ReserveWeightBps)
	w.U32(config.Monetary.StakeWeightBps)
	w.U32(config.Monetary.UtilizationWeightBps)
	w.U32(config.Monetary.VelocityWeightBps)
	w.U32(config.Monetary.OperationsWeightBps)

	w.U8(uint8(config.ComputeFeedback.Mode))
	w.U32(config.ComputeFeedback.BaseComputeRewardShareBps)
	w.U32(config.ComputeFeedback.MinComputeRewardShareBps)
	w.U32(config.ComputeFeedback.MaxComputeRewardShareBps)
	w.U32(config.ComputeFeedback.RewardSensitivityBps)
	w.U32(config.ComputeFeedback.MonetarySensitivityBps)
	w.U32(config.ComputeFeedback.MaxInflationCorrectionBps)
	return types.Hash(codec.DomainHash("zephyr/shadow-economics-config/v2", w.BytesCopy()))
}

func (e *ShadowEpochEngine) CheckpointBytes() ([]byte, error) {
	if e == nil || types.IsZero32([32]byte(e.Network)) {
		return nil, ErrShadowEpochEngine
	}
	var w codec.Writer
	w.U16(shadowEpochEngineCheckpointVersion)
	w.Fixed(e.Network[:])
	configHash := ShadowEpochEngineConfigHash(e.config)
	w.Fixed(configHash[:])
	writeComputeIndexSnapshot(&w, e.priorIndex)
	w.Bool(e.previous != nil)
	if e.previous != nil {
		raw, err := e.previous.CanonicalBytes()
		if err != nil {
			return nil, err
		}
		w.Bytes(raw)
	}
	return w.BytesCopy(), nil
}

func RestoreShadowEpochEngine(data []byte, expectedNetwork types.NetworkID, config ShadowEpochEngineConfig) (*ShadowEpochEngine, error) {
	if types.IsZero32([32]byte(expectedNetwork)) {
		return nil, ErrShadowEpochEngine
	}
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != shadowEpochEngineCheckpointVersion {
		return nil, ErrShadowEpochEngine
	}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return nil, ErrShadowEpochEngine
	}
	var network types.NetworkID
	copy(network[:], networkRaw)
	if network != expectedNetwork {
		return nil, ErrShadowEpochEngine
	}
	configRaw, err := r.Fixed(32)
	if err != nil {
		return nil, ErrShadowEpochEngine
	}
	var checkpointConfigHash types.Hash
	copy(checkpointConfigHash[:], configRaw)
	if checkpointConfigHash != ShadowEpochEngineConfigHash(config) {
		return nil, ErrShadowEpochEngine
	}
	priorIndex, err := readComputeIndexSnapshot(r)
	if err != nil {
		return nil, err
	}
	hasPrevious, err := r.Bool()
	if err != nil {
		return nil, ErrShadowEpochEngine
	}
	var previous *MonetaryEpochState
	if hasPrevious {
		raw, err := r.Bytes(64 << 10)
		if err != nil {
			return nil, ErrShadowEpochEngine
		}
		parsed, err := ParseMonetaryEpochState(raw)
		if err != nil || parsed.Network != expectedNetwork || parsed.Epoch != priorIndex.Epoch {
			return nil, ErrShadowEpochEngine
		}
		previous = &parsed
	} else if priorIndex != (ComputeIndexSnapshot{}) {
		return nil, ErrShadowEpochEngine
	}
	if r.Done() != nil {
		return nil, ErrShadowEpochEngine
	}
	engine, err := NewShadowEpochEngine(expectedNetwork, config)
	if err != nil {
		return nil, err
	}
	engine.priorIndex = priorIndex
	engine.previous = previous
	return engine, nil
}

func writeComputeIndexSnapshot(w *codec.Writer, snapshot ComputeIndexSnapshot) {
	w.U64(snapshot.Epoch)
	for class := compute.WorkClass(0); class < compute.WorkClassCount; class++ {
		w.U64(snapshot.ClassPriceQ9[class])
	}
	for class := compute.WorkClass(0); class < compute.WorkClassCount; class++ {
		w.U64(snapshot.ClassSamples[class])
	}
	w.U64(snapshot.BasketPriceQ9)
	w.U32(snapshot.CoverageBps)
	w.Bool(snapshot.Reliable)
	w.U64(snapshot.TotalSamples)
}

func readComputeIndexSnapshot(r *codec.Reader) (ComputeIndexSnapshot, error) {
	out := ComputeIndexSnapshot{}
	var err error
	out.Epoch, err = r.U64()
	if err != nil {
		return ComputeIndexSnapshot{}, ErrShadowEpochEngine
	}
	for class := compute.WorkClass(0); class < compute.WorkClassCount; class++ {
		out.ClassPriceQ9[class], err = r.U64()
		if err != nil {
			return ComputeIndexSnapshot{}, ErrShadowEpochEngine
		}
	}
	for class := compute.WorkClass(0); class < compute.WorkClassCount; class++ {
		out.ClassSamples[class], err = r.U64()
		if err != nil {
			return ComputeIndexSnapshot{}, ErrShadowEpochEngine
		}
	}
	out.BasketPriceQ9, err = r.U64()
	if err != nil {
		return ComputeIndexSnapshot{}, ErrShadowEpochEngine
	}
	out.CoverageBps, err = r.U32()
	if err != nil || out.CoverageBps > BasisPoints {
		return ComputeIndexSnapshot{}, ErrShadowEpochEngine
	}
	out.Reliable, err = r.Bool()
	if err != nil {
		return ComputeIndexSnapshot{}, ErrShadowEpochEngine
	}
	out.TotalSamples, err = r.U64()
	if err != nil {
		return ComputeIndexSnapshot{}, ErrShadowEpochEngine
	}
	if out.Epoch == 0 && out != (ComputeIndexSnapshot{}) {
		return ComputeIndexSnapshot{}, ErrShadowEpochEngine
	}
	return out, nil
}
