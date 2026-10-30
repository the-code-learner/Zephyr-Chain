package node

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// MultiSourceWitnessFloor requires agreement between distinct provisioned
// witness publications. Each source independently verifies a >2/3 Ed25519
// quorum using pinned keys. This is NOT a freshness oracle: identical stale
// signed bundles remain possible and must be checked against a separately
// authenticated latest-head/freshness policy before production recovery.
type MultiSourceWitnessFloor struct {
	sources []WitnessExternalFloorVerifier
}

// NewMultiSourceWitnessFloor requires at least two separately configured
// sources; callers must independently attest they run on distinct failure
// domains. No network URLs or source identities are inferred here.
func NewMultiSourceWitnessFloor(sources ...WitnessExternalFloorVerifier) (*MultiSourceWitnessFloor, error) {
	if len(sources) < 2 || len(sources) > 8 {
		return nil, ErrWitnessSigningIntent
	}
	out := make([]WitnessExternalFloorVerifier, len(sources))
	for i, source := range sources {
		if source == nil {
			return nil, ErrWitnessSigningIntent
		}
		out[i] = source
	}
	return &MultiSourceWitnessFloor{sources: out}, nil
}

// AuthenticatedWitnessFloor fails closed if any source is missing, invalid,
// conflicting, unavailable, or stale relative to another source. Distinct
// heights are conservatively rejected because quorum signatures do not
// themselves prove QC ancestry between different heights.
func (f *MultiSourceWitnessFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	if f == nil || len(f.sources) < 2 || len(f.sources) > 8 {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	var agreed FinalityCheckpoint
	for i, source := range f.sources {
		if source == nil {
			return FinalityCheckpoint{}, ErrWitnessSigningIntent
		}
		cp, err := source.AuthenticatedWitnessFloor()
		if err != nil {
			return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err)
		}
		if cp.Validate(cp.Network, cp.ValidatorRoot) != nil ||
			types.IsZero32([32]byte(cp.HeaderHash)) {
			return FinalityCheckpoint{}, ErrWitnessSigningIntent
		}
		if i > 0 && cp != agreed {
			return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ErrWitnessEquivocation)
		}
		agreed = cp
	}
	return agreed, nil
}
