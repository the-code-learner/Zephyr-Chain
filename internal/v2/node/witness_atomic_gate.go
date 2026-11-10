package node

import (
	"context"
	"errors"
	"time"

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ReserveAndReconcileWitnessIntentContext(ctx, j, cp, remote)
}

// ReserveAndReconcileWitnessIntentContext bounds context-aware remote reads
// under the SAME journal lock that fsyncs the pre-sign intent. The durable
// reservation is intentionally NOT rolled back when remote publication is
// unavailable or the request is cancelled. A successful return grants no
// signing capability and does not establish remote HSM antirollback.
//
// Legacy synchronous floor implementations and QC verifiers that do not
// honor context may still block: they are not production-safe adapters.
func ReserveAndReconcileWitnessIntentContext(ctx context.Context, j *WitnessSigningIntentJournal, cp FinalityCheckpoint, remote WitnessExternalFloorVerifier) error {
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
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	// A slow QC verifier can cancel the caller while returning nil.
	// Never create a new signing intent after a known cancellation.
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := j.store.advance(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	current, err := j.store.Current()
	if err != nil || current != cp {
		return ErrWitnessSigningIntent
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	floor, err := authenticatedWitnessFloorWithContext(ctx, remote)
	if err != nil || ctx.Err() != nil || floor.Validate(cp.Network, cp.ValidatorRoot) != nil ||
		floor != cp || types.IsZero32([32]byte(cp.HeaderHash)) {
		return errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
	}
	// Reverify after the remote network call, since a verifier can represent
	// a dynamic independent QC/finality source.
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	return nil
}
