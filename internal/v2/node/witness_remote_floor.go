package node

import (
	"errors"

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
	if j == nil || remote == nil {
		return ErrWitnessSigningIntent
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.store == nil || j.verifier == nil {
		return ErrWitnessSigningIntent
	}
	local, err := j.store.Current()
	if err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	floor, err := remote.AuthenticatedWitnessFloor()
	if err != nil || floor.Validate(local.Network, local.ValidatorRoot) != nil ||
		local.Height == 0 || floor != local ||
		types.IsZero32([32]byte(local.HeaderHash)) {
		return ErrWitnessSigningIntent
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(floor); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	return nil
}
