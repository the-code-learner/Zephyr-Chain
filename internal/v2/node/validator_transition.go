package node

import (
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ScheduleValidatorTransition commits the next committee root into a candidate
// before proposal signing. The current committee still signs/finalizes this
// header; the next committee only becomes active for the following height.
func ScheduleValidatorTransition(candidate *Candidate, current, next v2consensus.ValidatorSet) error {
	if candidate == nil || current.Network != candidate.Header.Network || next.Network != candidate.Header.Network {
		return ErrCandidateCert
	}
	currentRoot, err := current.Root()
	if err != nil || currentRoot != candidate.Header.ValidatorRoot {
		return ErrCandidateCert
	}
	nextRoot, err := next.Root()
	if err != nil || types.IsZero32([32]byte(nextRoot)) {
		return ErrCandidateCert
	}
	candidate.Header.NextValidatorRoot = nextRoot
	return nil
}

// CommitWithValidatorTransition finalizes a candidate with the current
// validator set and, only after that QC-backed commit succeeds, advances the
// runtime trust root for the next height. If no NextValidatorRoot is present,
// the current root remains active.
func (r *Runtime) CommitWithValidatorTransition(candidate Candidate, certificate v2consensus.Certificate, current v2consensus.ValidatorSet) (sharding.GlobalHeader, error) {
	finalized, err := r.Commit(candidate, certificate, current)
	if err != nil {
		return sharding.GlobalHeader{}, err
	}
	nextRoot := finalized.EffectiveNextValidatorRoot()
	r.mu.Lock()
	r.ValidatorRoot = nextRoot
	r.mu.Unlock()
	return finalized, nil
}
