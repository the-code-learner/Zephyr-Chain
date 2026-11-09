package node

import (
	"context"
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// SignedWitnessFloorSource supplies opaque canonical checkpoint envelopes.
// The source may be untrusted (including HTTP): quorum verification always
// happens locally using independently pinned witness public keys.
type SignedWitnessFloorSource interface {
	SignedWitnessFloor() ([]byte, error)
}

// QuorumAuthenticatedWitnessFloor connects the external high-water
// reconciliation gate to the existing strict Ed25519 >2/3 quorum verifier.
// Its trust policy is injected from a separately authenticated configuration;
// keys received from the source are never trusted to authorize themselves.
//
// This does not establish remote availability, signer key custody,
// monotonic remote publication, checkpoint freshness, or QC validity.
type QuorumAuthenticatedWitnessFloor struct {
	source        SignedWitnessFloorSource
	trustedKeys   [][]byte
	network       types.NetworkID
	validatorRoot types.Hash
}

// NewQuorumAuthenticatedWitnessFloor rejects malformed trust policies before
// accepting any external data. Actual signature checks occur on EVERY read.
func NewQuorumAuthenticatedWitnessFloor(source SignedWitnessFloorSource, keys [][]byte, network types.NetworkID, root types.Hash) (*QuorumAuthenticatedWitnessFloor, error) {
	if source == nil || len(keys) < minWitnessKeys || len(keys) > maxWitnessKeys ||
		types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(root)) {
		return nil, ErrWitnessSigningIntent
	}
	seen := make(map[string]struct{}, len(keys))
	copied := make([][]byte, 0, len(keys))
	for _, key := range keys {
		if len(key) != 32 {
			return nil, ErrWitnessSigningIntent
		}
		id := string(key)
		if _, ok := seen[id]; ok {
			return nil, ErrWitnessSigningIntent
		}
		seen[id] = struct{}{}
		copied = append(copied, append([]byte(nil), key...))
	}
	return &QuorumAuthenticatedWitnessFloor{source: source, trustedKeys: copied, network: network, validatorRoot: root}, nil
}

func (f *QuorumAuthenticatedWitnessFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.AuthenticatedWitnessFloorContext(context.Background())
}

// AuthenticatedWitnessFloorContext propagates cancellation to a context-aware
// source (including the production HTTPS source), and never authorizes a
// checkpoint after cancellation. Legacy sources remain supported for local
// fixtures but MUST provide their own bounded I/O in production.
func (f *QuorumAuthenticatedWitnessFloor) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	if f == nil || f.source == nil || ctx == nil || ctx.Err() != nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	var data []byte
	var err error
	if source, ok := f.source.(interface {
		SignedWitnessFloorContext(context.Context) ([]byte, error)
	}); ok {
		data, err = source.SignedWitnessFloorContext(ctx)
	} else {
		data, err = f.source.SignedWitnessFloor()
	}
	if err != nil || ctx.Err() != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
	}
	cp, err := ParseSignedWitnessCheckpoint(data, f.trustedKeys, f.network, f.validatorRoot)
	if err != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ErrWitnessCheckpoint)
	}
	if ctx.Err() != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ctx.Err())
	}
	return *cp, nil
}
