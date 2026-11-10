package node

import (
	"context"
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// WitnessExternalFloorVerifier is the operator-independent source of a signed
// high-water checkpoint. An implementation MUST authenticate its response
// against independently provisioned network, committee, and checkpoint keys.
// No locally restored file or unauthenticated RPC response is sufficient.
type WitnessExternalFloorVerifier interface {
	AuthenticatedWitnessFloor() (FinalityCheckpoint, error)
}

// ReconcileWitnessIntentWithExternalFloor fails closed unless a previously
// durable local signing intent exactly matches an authenticated remote floor.
// Exact matching is deliberate: a lower local file is possible rollback,
// while a greater local file without externally corroborated history could
// contain an unacknowledged signing operation. Neither may authorize keys.
//
// This is a recovery gate, NOT a remotely fenced signing service. The source
// of trust depends on an operator-provisioned independent floor verifier.
func ReconcileWitnessIntentWithExternalFloor(j *WitnessSigningIntentJournal, remote WitnessExternalFloorVerifier) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ReconcileWitnessIntentWithExternalFloorContext(ctx, j, remote)
}

// ReconcileWitnessIntentWithExternalFloorContext shares an operator-supplied
// deadline across the authenticated remote floor read and its QC recheck.
// Context-aware HTTPS/multi-source floors release the journal mutex after
// timeout. Legacy synchronous floors and QC verifiers which ignore context
// may still block, so they are not suitable for production remote recovery.
func ReconcileWitnessIntentWithExternalFloorContext(ctx context.Context, j *WitnessSigningIntentJournal, remote WitnessExternalFloorVerifier) error {
	if ctx == nil || j == nil || remote == nil {
		return ErrWitnessSigningIntent
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.store == nil || j.verifier == nil {
		return ErrWitnessSigningIntent
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	local, err := j.store.Current()
	if err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	floor, err := authenticatedWitnessFloorWithContext(ctx, remote)
	if err != nil || ctx.Err() != nil || floor.Validate(local.Network, local.ValidatorRoot) != nil ||
		local.Height == 0 || floor != local ||
		types.IsZero32([32]byte(local.HeaderHash)) {
		return errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(floor); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	// A verifier can cancel the parent context while performing its last
	// QC/history check. A cancelled recovery must not return authorization.
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	return nil
}

// authenticatedWitnessFloorWithContext centralizes the optional context-
// aware external floor boundary used by the non-signing journal gates.
// Production origins MUST implement the contextual API to be interruptible;
// the synchronous fallback exists only for legacy fixtures and adapters.
func authenticatedWitnessFloorWithContext(ctx context.Context, remote WitnessExternalFloorVerifier) (FinalityCheckpoint, error) {
	if ctx == nil || remote == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	if err := ctx.Err(); err != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	if contextual, ok := remote.(interface {
		AuthenticatedWitnessFloorContext(context.Context) (FinalityCheckpoint, error)
	}); ok {
		return contextual.AuthenticatedWitnessFloorContext(ctx)
	}
	return remote.AuthenticatedWitnessFloor()
}
