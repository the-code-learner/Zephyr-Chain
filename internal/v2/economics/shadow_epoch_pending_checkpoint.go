package economics

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const shadowEpochPendingCheckpointVersion uint16 = 1

func (p ShadowEpochPreview) PendingCheckpointBytes() ([]byte, error) {
	aggregateRaw, err := p.Aggregate.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	stateRaw, err := p.State.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	if p.ComputeIndex.Epoch != p.State.Epoch || p.Aggregate.Epoch != p.State.Epoch {
		return nil, ErrShadowEpochEngine
	}
	var w codec.Writer
	w.U16(shadowEpochPendingCheckpointVersion)
	w.Bytes(aggregateRaw)
	writeComputeIndexSnapshot(&w, p.ComputeIndex)
	w.Bytes(stateRaw)
	return w.BytesCopy(), nil
}

// RestoreShadowEpochPreview rebuilds the pending Merkle transition from the
// accepted engine history rather than trusting serialized consumed/created
// object lists. A cloned engine must be able to Accept the reconstructed
// preview before it is returned.
func RestoreShadowEpochPreview(data []byte, engine *ShadowEpochEngine) (ShadowEpochPreview, error) {
	if engine == nil {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != shadowEpochPendingCheckpointVersion {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}
	aggregateRaw, err := r.Bytes(64 << 10)
	if err != nil {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}
	aggregate, err := ParseEpochAggregate(aggregateRaw)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	index, err := readComputeIndexSnapshot(r)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	stateRaw, err := r.Bytes(64 << 10)
	if err != nil || r.Done() != nil {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}
	state, err := ParseMonetaryEpochState(stateRaw)
	if err != nil || state.Network != engine.Network || state.Epoch != aggregate.Epoch || index.Epoch != state.Epoch {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}

	var previousObject *object.Object
	if previous, ok := engine.PreviousState(); ok {
		obj, err := previous.Object()
		if err != nil {
			return ShadowEpochPreview{}, err
		}
		previousObject = &obj
	}
	consumed, created, err := ShadowMonetaryTransition(previousObject, state)
	if err != nil {
		return ShadowEpochPreview{}, err
	}
	preview := ShadowEpochPreview{
		Aggregate:       aggregate,
		ComputeIndex:    index,
		ComputeScarcity: ComputeScarcitySnapshot{Epoch: state.Epoch},
		State:           state,
		Consumed:        append([]types.ObjectID(nil), consumed...),
		Created:         append([]object.Object(nil), created...),
	}
	validator := engine.Clone()
	if validator == nil || validator.Accept(preview) != nil {
		return ShadowEpochPreview{}, ErrShadowEpochEngine
	}
	return preview, nil
}
