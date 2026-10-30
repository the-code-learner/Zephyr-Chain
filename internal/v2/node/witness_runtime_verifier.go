package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// RuntimeWitnessFinalityVerifier is an intentionally narrow witness finality
// proof adapter. It can verify ONLY an exact locally QC-certified, independently
// high-water-anchored tip, never an arbitrary externally provided checkpoint.
// Its snapshot is immutable, so future runtime progress cannot change an
// already authorized signing intent.
type RuntimeWitnessFinalityVerifier struct {
	certified FinalityCheckpoint
}

// NewRuntimeWitnessFinalityVerifier seals an observed tip only after the
// runtime has recovered (or durably committed) the certified history and its
// separate finality high-water ledger has caught up to that same tip.
// A freshly constructed or recovery-required runtime cannot mint this proof.
//
// This is NOT independent remote witness proof, cross-host key fencing,
// remote monotonic storage, or release authorization. The history and anchor
// still require authentic genesis and independent operator custody.
func NewRuntimeWitnessFinalityVerifier(r *Runtime) (*RuntimeWitnessFinalityVerifier, error) {
	if r == nil {
		return nil, ErrWitnessSigningIntent
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recoveryRequired || r.history == nil || r.anchorStore == nil ||
		r.globalCommitJournalPath == "" || r.Height == 0 ||
		types.IsZero32([32]byte(r.ParentHash)) {
		return nil, ErrWitnessSigningIntent
	}
	h := r.history
	h.mu.Lock()
	valid := !h.closed && !h.poisoned && h.count > 0 &&
		h.height == r.Height && h.parent == r.ParentHash &&
		h.network == r.Network && h.validatorRoot == r.ValidatorRoot
	h.mu.Unlock()
	if !valid {
		return nil, ErrWitnessSigningIntent
	}
	anchor, err := r.anchorStore.Current()
	if err != nil || anchor.Network != r.Network || anchor.ValidatorRoot != r.ValidatorRoot ||
		anchor.Height != r.Height || anchor.HeaderHash != r.ParentHash {
		return nil, errors.Join(ErrWitnessSigningIntent, ErrFinalityAnchor)
	}
	cp := FinalityCheckpoint{Network: r.Network, ValidatorRoot: r.ValidatorRoot,
		Height: r.Height, HeaderHash: r.ParentHash}
	if cp.Validate(r.Network, r.ValidatorRoot) != nil {
		return nil, ErrWitnessSigningIntent
	}
	return &RuntimeWitnessFinalityVerifier{certified: cp}, nil
}

func (v *RuntimeWitnessFinalityVerifier) VerifyFinalizedCheckpoint(cp FinalityCheckpoint) error {
	if v == nil || cp != v.certified {
		return ErrWitnessSigningIntent
	}
	return nil
}

// ReserveRuntimeVerifiedWitnessTip is the intended local integration path:
// only the tip sealed by the QC history and external high-water store can be
// durably reserved. This deliberately does not expose a signing operation.
// A production signer additionally needs globally fenced key custody.
func ReserveRuntimeVerifiedWitnessTip(j *WitnessSigningIntentJournal, verifier *RuntimeWitnessFinalityVerifier) error {
	if j == nil || verifier == nil {
		return ErrWitnessSigningIntent
	}
	// Require this journal to hold the exact sealed verifier, so a caller
	// cannot use an unrelated permissive verifier to authorize the tip.
	j.mu.Lock()
	match := !j.closed && j.verifier == verifier
	j.mu.Unlock()
	if !match {
		return ErrWitnessSigningIntent
	}
	return j.ReserveFinalizedIntent(verifier.certified)
}
