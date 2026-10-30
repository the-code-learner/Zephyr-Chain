package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// CertifiedWitnessFloorMinimum is a separately obtained, trusted minimum
// recovery checkpoint. It prevents sources which agree on an older signed
// checkpoint from silently authorizing recovery below a known floor.
//
// The minimum MUST be authenticated from an independent, rollback-resistant
// channel (e.g. an external monotonic high-water record). This adapter does
// NOT discover the chain head or prove wall-clock freshness.
type CertifiedWitnessFloorMinimum struct {
	remote WitnessExternalFloorVerifier
	floor  FinalityCheckpoint
}

func NewCertifiedWitnessFloorMinimum(remote WitnessExternalFloorVerifier, floor FinalityCheckpoint) (*CertifiedWitnessFloorMinimum, error) {
	if remote == nil || floor.Validate(floor.Network, floor.ValidatorRoot) != nil ||
		types.IsZero32([32]byte(floor.HeaderHash)) {
		return nil, ErrWitnessSigningIntent
	}
	return &CertifiedWitnessFloorMinimum{remote: remote, floor: floor}, nil
}

// AuthenticatedWitnessFloor rejects an old signed checkpoint as well as a
// competing signed checkpoint at the minimum height. A higher checkpoint is
// NOT an ancestry proof: callers must independently verify QC-certified
// history before accepting a higher value.
func (f *CertifiedWitnessFloorMinimum) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	if f == nil || f.remote == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	cp, err := f.remote.AuthenticatedWitnessFloor()
	if err != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	if cp.Network != f.floor.Network || cp.ValidatorRoot != f.floor.ValidatorRoot ||
		cp.Height < f.floor.Height || types.IsZero32([32]byte(cp.HeaderHash)) {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	if cp.Height == f.floor.Height && cp.HeaderHash != f.floor.HeaderHash {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ErrWitnessEquivocation)
	}
	return cp, nil
}
