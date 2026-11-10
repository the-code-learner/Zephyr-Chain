package node

import (
	"context"
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
	return f.AuthenticatedWitnessFloorContext(context.Background())
}

// AuthenticatedWitnessFloorContext propagates caller cancellation through
// the externally pinned floor gate to context-aware remote witness sources.
// A late response must never authorize recovery after the original deadline.
// Legacy synchronous adapters cannot interrupt their own I/O, so the
// cancellation check is repeated before accepting their results.
func (f *CertifiedWitnessFloorMinimum) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	if f == nil || f.remote == nil || ctx == nil || ctx.Err() != nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	var cp FinalityCheckpoint
	var err error
	if remote, ok := f.remote.(interface {
		AuthenticatedWitnessFloorContext(context.Context) (FinalityCheckpoint, error)
	}); ok {
		cp, err = remote.AuthenticatedWitnessFloorContext(ctx)
	} else {
		cp, err = f.remote.AuthenticatedWitnessFloor()
	}
	if err != nil || ctx.Err() != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
	}
	if cp.Network != f.floor.Network || cp.ValidatorRoot != f.floor.ValidatorRoot ||
		cp.Height < f.floor.Height || types.IsZero32([32]byte(cp.HeaderHash)) {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	if cp.Height == f.floor.Height && cp.HeaderHash != f.floor.HeaderHash {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ErrWitnessEquivocation)
	}
	if ctx.Err() != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ctx.Err())
	}
	return cp, nil
}
