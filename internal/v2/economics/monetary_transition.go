package economics

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ShadowMonetaryTransition returns the object delta for an epoch transition.
// It does not mutate a store. The caller may include this delta in candidate
// state simulation and apply it only after normal consensus finality.
func ShadowMonetaryTransition(previous *object.Object, next MonetaryEpochState) ([]types.ObjectID, []object.Object, error) {
	if err := next.Validate(); err != nil {
		return nil, nil, err
	}
	nextObject, err := next.Object()
	if err != nil {
		return nil, nil, err
	}
	if previous == nil {
		if next.Epoch != 1 || !types.IsZero32([32]byte(next.PreviousStateHash)) {
			return nil, nil, ErrMonetaryState
		}
		return nil, []object.Object{nextObject}, nil
	}
	if previous.ID != nextObject.ID || previous.Kind != object.KindSystem {
		return nil, nil, ErrMonetaryState
	}
	priorState, err := ParseMonetaryEpochState(previous.Data)
	if err != nil || priorState.Network != next.Network || priorState.Epoch+1 != next.Epoch || previous.Version != priorState.Epoch {
		return nil, nil, ErrMonetaryState
	}
	priorHash, err := priorState.Hash()
	if err != nil || next.PreviousStateHash != priorHash {
		return nil, nil, ErrMonetaryState
	}
	return []types.ObjectID{previous.ID}, []object.Object{nextObject}, nil
}
