package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrWitnessEvidenceSet = errors.New("invalid or incomplete witness checkpoint evidence set")

// VerifyWitnessEvidenceSet authenticates every supplied signed bundle against
// exactly the same independently pinned policy and rejects any two valid
// claims of different finalized hashes at the same height. It never selects
// an arbitrary winner. No untrusted bundle may be silently skipped.
//
// This is an offline evidence check, NOT proof of witness independence,
// freshness, remote availability, QC ancestry or anti-rollback storage.
// Recovery must separately establish those properties before using a claim.
func VerifyWitnessEvidenceSet(bundles [][]byte, trustedKeys [][]byte, network types.NetworkID, validatorRoot types.Hash) ([]FinalityCheckpoint, error) {
	if len(bundles) == 0 || len(bundles) > 64 {
		return nil, ErrWitnessEvidenceSet
	}
	claims := make([]FinalityCheckpoint, 0, len(bundles))
	byHeight := make(map[uint64]types.Hash, len(bundles))
	for _, data := range bundles {
		cp, err := ParseSignedWitnessCheckpoint(data, trustedKeys, network, validatorRoot)
		if err != nil {
			return nil, errors.Join(ErrWitnessEvidenceSet, ErrWitnessCheckpoint)
		}
		if hash, ok := byHeight[cp.Height]; ok && hash != cp.HeaderHash {
			return nil, errors.Join(ErrWitnessEvidenceSet, ErrWitnessEquivocation)
		}
		byHeight[cp.Height] = cp.HeaderHash
		claims = append(claims, *cp)
	}
	return claims, nil
}
