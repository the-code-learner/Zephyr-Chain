package node

import (
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ErrWitnessSigningIntent means that an unverified, conflicting, rolled-back,
// or locally unavailable signing intent MUST NOT be passed to a key custodian.
var ErrWitnessSigningIntent = errors.New("witness signing intent not durably authorized")

// WitnessFinalityVerifier is the trust boundary for independent, genesis-pinned
// QC history. Production callers must NOT implement this with an RPC assertion,
// signature alone, wall-clock, or a manually supplied (height,hash) tuple.
// This interface is intentionally separate from any private-key access.
type WitnessFinalityVerifier interface {
	VerifyFinalizedCheckpoint(FinalityCheckpoint) error
}

// WitnessSigningIntentJournal is a local non-key-bearing prototype of a
// durable pre-sign reservation. It deliberately has NO Sign method.
// The underlying store uses exclusive flock, append-only hash chaining,
// fsync and strict monotonic height / equal-height conflict rejection.
//
// WARNING: local flock cannot fence two machines with distinct disks; a
// remotely fenced signer or HSM is mandatory before production key use.
// Rolled-back copies of both local files cannot be detected by this journal.
type WitnessSigningIntentJournal struct {
	mu       sync.Mutex
	store    *FinalityAnchorStore
	verifier WitnessFinalityVerifier
	closed   bool
}

// OpenWitnessSigningIntentJournal requires a separately provisioned, private
// absolute-path file and a verifier that independently validates QC ancestry.
// A verifier is NOT inferred from the caller's claimed checkpoint.
func OpenWitnessSigningIntentJournal(path string, network types.NetworkID, root types.Hash, create bool, verifier WitnessFinalityVerifier) (*WitnessSigningIntentJournal, error) {
	if verifier == nil {
		return nil, ErrWitnessSigningIntent
	}
	store, err := OpenFinalityAnchorStore(path, network, root, create)
	if err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	return &WitnessSigningIntentJournal{store: store, verifier: verifier}, nil
}

// ReserveFinalizedIntent checks the independent QC proof BEFORE appending
// and fsyncing the intent. This returns NO signature or signing capability.
// Repeated same-height/same-hash reservations are idempotent. Other
// same-height hashes, decreasing heights and failed syncs fail closed.
func (j *WitnessSigningIntentJournal) ReserveFinalizedIntent(cp FinalityCheckpoint) error {
	if j == nil {
		return ErrWitnessSigningIntent
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.verifier == nil || j.store == nil {
		return ErrWitnessSigningIntent
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := j.store.advance(cp); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	return nil
}

func (j *WitnessSigningIntentJournal) Current() (FinalityCheckpoint, error) {
	if j == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.store == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	cp, err := j.store.Current()
	if err != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	return cp, nil
}

func (j *WitnessSigningIntentJournal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	return j.store.Close()
}
