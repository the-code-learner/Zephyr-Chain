package economics

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrShadowEpochEngine = errors.New("invalid Zephyr shadow epoch transition")

type ShadowEpochEngineConfig struct {
	ComputeIndex    ComputeIndexConfig
	ComputeScarcity ComputeScarcityConfig
	Monetary        MonetaryPolicy
	ComputeFeedback ComputeFeedbackPolicy
}

type MonetaryBalanceSnapshot struct {
	TotalSupply     uint64
	StakedSupply    uint64
	ProtocolReserve uint64
	BaseFee         uint64
}

type ShadowEpochPreview struct {
	Aggregate            EpochAggregate
	ComputeIndex         ComputeIndexSnapshot
	ComputePriceTrendBps int32
	ComputeScarcity      ComputeScarcitySnapshot
	MonetaryDecision     MonetaryDecision
	ComputeFeedback      ComputeFeedbackDecision
	State                MonetaryEpochState
	Consumed             []types.ObjectID
	Created              []object.Object
}

type ShadowEpochEngine struct {
	Network    types.NetworkID
	config     ShadowEpochEngineConfig
	priorIndex ComputeIndexSnapshot
	previous   *MonetaryEpochState
}

func NewShadowEpochEngine(network types.NetworkID, config ShadowEpochEngineConfig) (*ShadowEpochEngine, error) {
	if types.IsZero32([32]byte(network)) || config.Monetary.EpochsPerYear == 0 || config.Monetary.OperationsTarget == 0 ||
		config.ComputeScarcity.MaxAbsScoreBps == 0 || config.ComputeFeedback.Mode > ComputeFeedbackMonetaryBand {
		return nil, ErrShadowEpochEngine
	}
	return &ShadowEpochEngine{Network: network, config: config}, nil
}

// PreviewCloseEpoch deterministically composes finalized shard telemetry into
// ZCPI, ZCSI, ZAMP and a Merkle-authenticated shadow MonetaryEpochState. It does
// not advance controller history. The returned object delta must be included in
// a normal consensus candidate and finalized before Accept is called.
func (e *ShadowEpochEngine) PreviewCloseEpoch(metrics []ShardEpochMetrics, verifiedWork []compute.VerifiedWork, balances MonetaryBalanceSnapshot) (ShadowEpochPreview, error) {
	if e == nil {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}
	aggregate, err := AggregateEpochMetrics(metrics)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	if e.previous == nil {
		if aggregate.Epoch != 1 {
			return ShadowEpochPreview{}, ErrShadowEpochEngine
		}
	} else if aggregate.Epoch != e.previous.Epoch+1 {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}

	index, err := BuildComputeIndex(aggregate.Epoch, verifiedWork, e.priorIndex, e.config.ComputeIndex)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	trend := ComputePriceTrendBps(index.BasketPriceQ9, e.priorIndex.BasketPriceQ9)
	scarcity, err := BuildComputeScarcity(aggregate.Epoch, aggregate.ComputeMarketMetrics(trend, index.Reliable), e.config.ComputeScarcity)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	state, monetary, feedback, err := BuildShadowMonetaryEpochState(
		e.Network,
		e.previous,
		aggregate,
		balances.TotalSupply,
		balances.StakedSupply,
		balances.ProtocolReserve,
		index.BasketPriceQ9,
		trend,
		index.Reliable,
		scarcity,
		e.config.Monetary,
		e.config.ComputeFeedback,
		balances.BaseFee,
	)
	if err != nil {
		return ShadowEpochPreview{}, err
	}

	var previousObject *object.Object
	if e.previous != nil {
		prior, err := e.previous.Object()
		if err != nil {
			return ShadowEpochPreview{}, err
		}
		previousObject = &prior
	}
	consumed, created, err := ShadowMonetaryTransition(previousObject, state)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	return ShadowEpochPreview{
		Aggregate: aggregate, ComputeIndex: index, ComputePriceTrendBps: trend,
		ComputeScarcity: scarcity, MonetaryDecision: monetary, ComputeFeedback: feedback,
		State: state, Consumed: consumed, Created: created,
	}, nil
}

// Accept advances the shadow controller only after the caller has finalized the
// exact monetary object returned by PreviewCloseEpoch.
func (e *ShadowEpochEngine) Accept(preview ShadowEpochPreview) error {
	if e == nil || preview.State.Network != e.Network || preview.State.Epoch != preview.Aggregate.Epoch ||
		preview.ComputeIndex.Epoch != preview.State.Epoch || preview.ComputeScarcity.Epoch != preview.State.Epoch ||
		preview.State.Validate() != nil {
		return ErrShadowEpochEngine
	}
	if e.previous == nil {
		if preview.State.Epoch != 1 || !types.IsZero32([32]byte(preview.State.PreviousStateHash)) {
			return ErrShadowEpochEngine
		}
	} else {
		if preview.State.Epoch != e.previous.Epoch+1 {
			return ErrShadowEpochEngine
		}
		priorHash, err := e.previous.Hash()
		if err != nil || preview.State.PreviousStateHash != priorHash {
			return ErrShadowEpochEngine
		}
	}
	aggregateHash, err := preview.Aggregate.Hash()
	if err != nil || aggregateHash != preview.State.AggregateHash {
		return ErrShadowEpochEngine
	}
	if preview.State.ComputeIndexQ9 != preview.ComputeIndex.BasketPriceQ9 || preview.State.ComputeIndexReliable != preview.ComputeIndex.Reliable {
		return ErrShadowEpochEngine
	}

	stateCopy := preview.State
	e.previous = &stateCopy
	e.priorIndex = preview.ComputeIndex
	return nil
}

func (e *ShadowEpochEngine) PreviousState() (MonetaryEpochState, bool) {
	if e == nil || e.previous == nil {
		return MonetaryEpochState{}, false
	}
	return *e.previous, true
}

func (e *ShadowEpochEngine) PriorComputeIndex() ComputeIndexSnapshot {
	if e == nil {
		return ComputeIndexSnapshot{}
	}
	return e.priorIndex
}
