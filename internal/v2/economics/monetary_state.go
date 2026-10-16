package economics

import (
	"encoding/binary"
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const MonetaryEpochStateVersion uint16 = 1

var ErrMonetaryState = errors.New("invalid Zephyr shadow monetary epoch state")

type MonetaryEpochState struct {
	Version                         uint16
	Network                         types.NetworkID
	Epoch                           uint64
	Shadow                          bool
	TotalSupply                     uint64
	CirculatingSupply               uint64
	StakedSupply                    uint64
	ProtocolReserve                 uint64
	ZAMPBaseTargetBps               uint32
	SuggestedTargetBps              uint32
	BaseFee                         uint64
	AggregateHash                   types.Hash
	ComputeIndexQ9                  uint64
	ComputeIndexReliable            bool
	ComputeScarcityMagnitudeBps     uint32
	ComputeScarcityNegative         bool
	ComputeScarcityReliable         bool
	FeedbackMode                    ComputeFeedbackMode
	ShadowGrossMintTarget           uint64
	ShadowComputeIncentiveMint      uint64
	InflationCorrectionMagnitudeBps uint32
	InflationCorrectionNegative     bool
	PreviousStateHash               types.Hash
}

func (s MonetaryEpochState) Validate() error {
	if s.Version != MonetaryEpochStateVersion || types.IsZero32([32]byte(s.Network)) || s.Epoch == 0 || !s.Shadow ||
		s.TotalSupply == 0 || s.CirculatingSupply == 0 || s.CirculatingSupply > s.TotalSupply ||
		s.StakedSupply > s.CirculatingSupply || s.ProtocolReserve > s.TotalSupply ||
		s.ZAMPBaseTargetBps > BasisPoints || s.SuggestedTargetBps > BasisPoints ||
		types.IsZero32([32]byte(s.AggregateHash)) || s.ComputeScarcityMagnitudeBps > BasisPoints ||
		s.FeedbackMode > ComputeFeedbackMonetaryBand || s.InflationCorrectionMagnitudeBps > BasisPoints {
		return ErrMonetaryState
	}
	return nil
}

func (s MonetaryEpochState) CanonicalBytes() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(s.Version)
	w.Fixed(s.Network[:])
	w.U64(s.Epoch)
	w.Bool(s.Shadow)
	w.U64(s.TotalSupply)
	w.U64(s.CirculatingSupply)
	w.U64(s.StakedSupply)
	w.U64(s.ProtocolReserve)
	w.U32(s.ZAMPBaseTargetBps)
	w.U32(s.SuggestedTargetBps)
	w.U64(s.BaseFee)
	w.Fixed(s.AggregateHash[:])
	w.U64(s.ComputeIndexQ9)
	w.Bool(s.ComputeIndexReliable)
	w.U32(s.ComputeScarcityMagnitudeBps)
	w.Bool(s.ComputeScarcityNegative)
	w.Bool(s.ComputeScarcityReliable)
	w.U8(uint8(s.FeedbackMode))
	w.U64(s.ShadowGrossMintTarget)
	w.U64(s.ShadowComputeIncentiveMint)
	w.U32(s.InflationCorrectionMagnitudeBps)
	w.Bool(s.InflationCorrectionNegative)
	w.Fixed(s.PreviousStateHash[:])
	return w.BytesCopy(), nil
}

func ParseMonetaryEpochState(data []byte) (MonetaryEpochState, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	epoch, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	shadow, err := r.Bool()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	totalSupply, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	circulating, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	staked, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	reserve, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	baseTarget, err := r.U32()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	suggestedTarget, err := r.U32()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	baseFee, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	aggregateRaw, err := r.Fixed(32)
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	computeIndex, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	computeIndexReliable, err := r.Bool()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	scarcityMagnitude, err := r.U32()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	scarcityNegative, err := r.Bool()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	scarcityReliable, err := r.Bool()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	mode, err := r.U8()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	grossMint, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	computeMint, err := r.U64()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	correctionMagnitude, err := r.U32()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	correctionNegative, err := r.Bool()
	if err != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	previousRaw, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	var network types.NetworkID
	var aggregate, previous types.Hash
	copy(network[:], networkRaw)
	copy(aggregate[:], aggregateRaw)
	copy(previous[:], previousRaw)
	out := MonetaryEpochState{
		Version: MonetaryEpochStateVersion, Network: network, Epoch: epoch, Shadow: shadow,
		TotalSupply: totalSupply, CirculatingSupply: circulating, StakedSupply: staked, ProtocolReserve: reserve,
		ZAMPBaseTargetBps: baseTarget, SuggestedTargetBps: suggestedTarget, BaseFee: baseFee,
		AggregateHash: aggregate, ComputeIndexQ9: computeIndex, ComputeIndexReliable: computeIndexReliable,
		ComputeScarcityMagnitudeBps: scarcityMagnitude, ComputeScarcityNegative: scarcityNegative,
		ComputeScarcityReliable: scarcityReliable, FeedbackMode: ComputeFeedbackMode(mode),
		ShadowGrossMintTarget: grossMint, ShadowComputeIncentiveMint: computeMint,
		InflationCorrectionMagnitudeBps: correctionMagnitude, InflationCorrectionNegative: correctionNegative,
		PreviousStateHash: previous,
	}
	if version != MonetaryEpochStateVersion || out.Validate() != nil {
		return MonetaryEpochState{}, ErrMonetaryState
	}
	return out, nil
}

func (s MonetaryEpochState) Hash() (types.Hash, error) {
	raw, err := s.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/monetary-epoch-state/v2", raw)), nil
}

func MonetaryStateObjectID(network types.NetworkID) types.ObjectID {
	var w codec.Writer
	w.Fixed(network[:])
	w.String("monetary-epoch")
	hash := codec.DomainHash("zephyr/system-object-id/v2", w.BytesCopy())
	binary.BigEndian.PutUint32(hash[:4], 0)
	return types.ObjectID(hash)
}

func (s MonetaryEpochState) Object() (object.Object, error) {
	raw, err := s.CanonicalBytes()
	if err != nil {
		return object.Object{}, err
	}
	return object.Object{
		ID: MonetaryStateObjectID(s.Network), Version: s.Epoch,
		Kind: object.KindSystem, Data: raw,
	}, nil
}

func BuildShadowMonetaryEpochState(
	network types.NetworkID,
	previous *MonetaryEpochState,
	aggregate EpochAggregate,
	totalSupply uint64,
	stakedSupply uint64,
	protocolReserve uint64,
	computeIndexQ9 uint64,
	computePriceTrendBps int32,
	computeIndexReliable bool,
	scarcity ComputeScarcitySnapshot,
	monetaryPolicy MonetaryPolicy,
	feedbackPolicy ComputeFeedbackPolicy,
	baseFee uint64,
) (MonetaryEpochState, MonetaryDecision, ComputeFeedbackDecision, error) {
	if types.IsZero32([32]byte(network)) || scarcity.Epoch != aggregate.Epoch {
		return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, ErrMonetaryState
	}
	aggregateHash, err := aggregate.Hash()
	if err != nil {
		return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, err
	}
	metrics, err := aggregate.MonetaryMetrics(totalSupply, stakedSupply, protocolReserve, computeIndexQ9, computePriceTrendBps, computeIndexReliable)
	if err != nil {
		return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, err
	}
	priorTarget := monetaryPolicy.TargetInflationBps
	var previousHash types.Hash
	if previous != nil {
		if previous.Network != network || previous.Epoch+1 != aggregate.Epoch || previous.Validate() != nil {
			return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, ErrMonetaryState
		}
		priorTarget = previous.ZAMPBaseTargetBps
		previousHash, err = previous.Hash()
		if err != nil {
			return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, err
		}
	}
	monetaryDecision, err := EvaluateShadow(priorTarget, metrics, monetaryPolicy)
	if err != nil {
		return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, err
	}
	feedback, err := EvaluateComputeFeedback(monetaryDecision, metrics, monetaryPolicy, scarcity, feedbackPolicy)
	if err != nil {
		return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, err
	}
	scarcityMagnitude, scarcityNegative := signedMagnitude(scarcity.ScoreBps)
	correctionMagnitude, correctionNegative := signedMagnitude(feedback.InflationCorrectionBps)
	state := MonetaryEpochState{
		Version: MonetaryEpochStateVersion, Network: network, Epoch: aggregate.Epoch, Shadow: true,
		TotalSupply: totalSupply, CirculatingSupply: aggregate.CirculatingNativeSupply,
		StakedSupply: stakedSupply, ProtocolReserve: protocolReserve,
		ZAMPBaseTargetBps: monetaryDecision.TargetInflationBps, SuggestedTargetBps: feedback.SuggestedTargetInflationBps,
		BaseFee: baseFee, AggregateHash: aggregateHash, ComputeIndexQ9: computeIndexQ9, ComputeIndexReliable: computeIndexReliable,
		ComputeScarcityMagnitudeBps: scarcityMagnitude, ComputeScarcityNegative: scarcityNegative,
		ComputeScarcityReliable: scarcity.Reliable, FeedbackMode: feedbackPolicy.Mode,
		ShadowGrossMintTarget: feedback.SuggestedGrossMint, ShadowComputeIncentiveMint: feedback.SuggestedComputeIncentiveMint,
		InflationCorrectionMagnitudeBps: correctionMagnitude, InflationCorrectionNegative: correctionNegative,
		PreviousStateHash: previousHash,
	}
	if err := state.Validate(); err != nil {
		return MonetaryEpochState{}, MonetaryDecision{}, ComputeFeedbackDecision{}, err
	}
	return state, monetaryDecision, feedback, nil
}

func signedMagnitude(value int32) (uint32, bool) {
	if value < 0 {
		return uint32(-int64(value)), true
	}
	return uint32(value), false
}
