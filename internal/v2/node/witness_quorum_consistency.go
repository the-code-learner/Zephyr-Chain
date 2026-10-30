package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ErrWitnessEquivocation marks two independently valid supermajority witness
// bundles that assert different finalized headers at the exact same height.
// Callers must halt recovery and retain both bundles for investigation.
var ErrWitnessEquivocation = errors.New("conflicting valid witness finality quorums at equal height")

// CheckWitnessQuorumConsistency authenticates both canonical envelopes under
// the SAME independently pinned witness key policy. It detects direct
// equal-height finality contradictions, rather than choosing the newer input.
//
// A nil result does NOT prove ancestry for distinct heights, current freshness,
// independent witness custody or correctness of the asserted QCs. Recovery
// MUST continue to replay QC-certified history and its separate high-water mark.
func CheckWitnessQuorumConsistency(first, second []byte, trustedKeys [][]byte, network types.NetworkID, validatorRoot types.Hash) error {
	a, err := ParseSignedWitnessCheckpoint(first, trustedKeys, network, validatorRoot)
	if err != nil {
		return ErrWitnessCheckpoint
	}
	b, err := ParseSignedWitnessCheckpoint(second, trustedKeys, network, validatorRoot)
	if err != nil {
		return ErrWitnessCheckpoint
	}
	if a.Height == b.Height && a.HeaderHash != b.HeaderHash {
		return ErrWitnessEquivocation
	}
	return nil
}
