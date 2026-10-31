package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ReserveAndReconcileWitnessIntent atomically binds local QC verification,
// durable reservation and independently authenticated external high-water
// reconciliation under one journal lock. The local intent is fsynced before
// contacting the remote witness floor, so a remote outage cannot authorize
// signing or erase an already-reserved local intent.
//
// A failure may leave a durable local reservation without a matching remote
// publication; operators must reconcile/recover it before any key use.
// Crucially, a successful return does NOT grant signing permission: a
// production signing custodian still needs a globally linearizable remote
// reservation/fencing protocol. No private key is accessed here.
func ReserveAndReconcileWitnessIntent(j *WitnessSigningIntentJournal, cp FinalityCheckpoint, remote WitnessExternalFloorVerifier) error {
	if j == nil || remote == nil {
		return ErrWitnessSigningIntent
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.store == nil || j.verifier == nil {
		return ErrWitnessSigningIntent
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := j.store.advance(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	current, err := j.store.Current()
	if err != nil || current != cp {
		return ErrWitnessSigningIntent
	}
	floor, err := remote.AuthenticatedWitnessFloor()
	if err != nil || floor.Validate(cp.Network, cp.ValidatorRoot) != nil ||
		floor != cp || types.IsZero32([32]byte(cp.HeaderHash)) {
		return ErrWitnessSigningIntent
	}
	// Reverify after the remote network call, since a verifier can represent
	// a dynamic independent QC/finality source.
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	return nil
}
