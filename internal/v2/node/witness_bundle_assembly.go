package node

import (
	"encoding/hex"
	"encoding/json"
	"sort"
)

// AssembleSignedWitnessCheckpoint builds a canonical publication envelope
// from DETACHED signatures produced by separate, QC-verifying witnesses.
// It neither signs checkpoints nor authorizes a key custodian. Before using
// this in production, each signing witness must independently verify
// genesis-anchored QC history and durably fence its key across all hosts.
// A valid bundle proves only signature provenance, not freshness or
// physical operator independence.
func AssembleSignedWitnessCheckpoint(cp FinalityCheckpoint, trustedKeys [][]byte, signatures []WitnessSignature) ([]byte, error) {
	if err := cp.Validate(cp.Network, cp.ValidatorRoot); err != nil ||
		len(signatures) < minWitnessKeys || len(signatures) > maxWitnessKeys {
		return nil, ErrWitnessCheckpoint
	}

	envelope := SignedWitnessCheckpoint{
		NetworkID:     hex.EncodeToString(cp.Network[:]),
		ValidatorRoot: hex.EncodeToString(cp.ValidatorRoot[:]),
		Height:        cp.Height,
		HeaderHash:    hex.EncodeToString(cp.HeaderHash[:]),
		Signatures:    append([]WitnessSignature(nil), signatures...),
	}
	// Do not reorder caller-owned attestations. The strict parser enforces
	// distinct pinned keys, every signature, and the >2/3 key threshold.
	sort.Slice(envelope.Signatures, func(i, j int) bool {
		return envelope.Signatures[i].PublicKey < envelope.Signatures[j].PublicKey
	})
	payload, err := json.Marshal(envelope)
	if err != nil || len(payload) > maxWitnessBundleBytes {
		return nil, ErrWitnessCheckpoint
	}
	verified, err := ParseSignedWitnessCheckpoint(payload, trustedKeys, cp.Network, cp.ValidatorRoot)
	if err != nil || verified == nil || *verified != cp {
		return nil, ErrWitnessCheckpoint
	}
	return payload, nil
}
