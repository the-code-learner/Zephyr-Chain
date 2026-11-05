package node

import (
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// MultiSourceWitnessFloor requires agreement between distinct provisioned
// witness publications. Each source independently verifies a >2/3 Ed25519
// quorum using pinned keys. The highest observed checkpoint is kept in
// memory, rejecting later agreed rollback or equivocation within one process.
// This is NOT a freshness oracle: an initially stale signed bundle or a
// restart with rolled-back local storage still requires an independently
// authenticated durable high-water/freshness policy before recovery.
type MultiSourceWitnessFloor struct {
	sources  []WitnessExternalFloorVerifier
	mu       sync.Mutex
	observed bool
	last     FinalityCheckpoint
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
	// Serialize the observation update: concurrent reads must never allow
	// an older signed checkpoint to overwrite a newer accepted one.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.observed {
		if agreed.Network != f.last.Network || agreed.ValidatorRoot != f.last.ValidatorRoot ||
			agreed.Height < f.last.Height {
			return FinalityCheckpoint{}, ErrWitnessSigningIntent
		}
		if agreed.Height == f.last.Height && agreed.HeaderHash != f.last.HeaderHash {
			return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ErrWitnessEquivocation)
		}
	}
	f.last = agreed
	f.observed = true
	return agreed, nil
}
