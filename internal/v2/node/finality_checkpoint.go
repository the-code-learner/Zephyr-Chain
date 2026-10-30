package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrFinalityCheckpoint = errors.New("untrusted or unproven external finalized checkpoint")

// FinalityCheckpoint is an independently obtained *minimum* finalized
// checkpoint. A protected observer must prove this exact (height, hash)
// occurs in its QC-verified local genesis-to-tip history before startup.
//
// The values are trust inputs supplied by the operator from outside the
// recoverable state directory. This struct is NOT a proof of freshness,
// external signature, remote agreement or monotonic checkpoint storage.
type FinalityCheckpoint struct {
	Network       types.NetworkID
	ValidatorRoot types.Hash
	Height        uint64
	HeaderHash    types.Hash
}

func (c FinalityCheckpoint) Validate(network types.NetworkID, validatorRoot types.Hash) error {
	if c.Height == 0 ||
		types.IsZero32([32]byte(c.HeaderHash)) ||
		types.IsZero32([32]byte(c.Network)) ||
		types.IsZero32([32]byte(c.ValidatorRoot)) ||
		c.Network != network || c.ValidatorRoot != validatorRoot {
		return ErrFinalityCheckpoint
	}
	return nil
}
