package node

import (
	"crypto/ed25519"
	"crypto/sha256"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	witnessHTTPSChallengeHeader = "X-Zephyr-Witness-Challenge"
	witnessHTTPSOperatorSignatureHeader = "X-Zephyr-Witness-Operator-Signature"
)

// witnessHTTPSChallengePolicy is installed only by the constructor which
// verifies the independently pinned, Ed25519-signed operator inventory.
type witnessHTTPSChallengePolicy struct {
	network       types.NetworkID
	validatorRoot types.Hash
	operatorID    string
	revision      uint64
	operatorKey   ed25519.PublicKey
}

// WitnessHTTPSPublicationChallengeDigest lets an independently provisioned
// HTTPS operator authenticate one exact request/response exchange. A fresh
// random client challenge binds the operator's inventory-signing identity to
// the bytes of a quorum-signed checkpoint bundle, genesis, operator label and
// pinned inventory revision. The digest is domain-separated from inventory
// and consensus checkpoint signing. An HTTPS publisher signs this digest
// with its independently held OPERATOR inventory key and returns the
// lowercase-hex signature in X-Zephyr-Witness-Operator-Signature.
//
// This proves live possession of that key in response to an unpredictable
// challenge. It does NOT prove that the returned checkpoint is recent,
// nonrollbackable, or backed by locally verified QC history. Independent
// global high-water, real operator separation and HSM/audits are mandatory.
func WitnessHTTPSPublicationChallengeDigest(
	network types.NetworkID,
	validatorRoot types.Hash,
	operatorID string,
	revision uint64,
	challenge [32]byte,
	payload []byte,
) ([32]byte, error) {
	if types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validatorRoot)) ||
		!validWitnessPublicationOperatorID(operatorID) || revision == 0 ||
		isZeroWitnessFenceChallenge(challenge) || len(payload) == 0 ||
		len(payload) > maxWitnessBundleBytes {
		return [32]byte{}, ErrWitnessSigningIntent
	}
	payloadHash := sha256.Sum256(payload)
	var writer codec.Writer
	writer.Fixed(network[:])
	writer.Fixed(validatorRoot[:])
	writer.String(operatorID)
	writer.U64(revision)
	writer.Fixed(challenge[:])
	writer.Fixed(payloadHash[:])
	return codec.DomainHash("zephyr/https-witness-live-publication/v1", writer.BytesCopy()), nil
}

// NewChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor opts in to a
// challenge-bound HTTPS publication protocol. First verify the complete
// signed operator inventory, its out-of-band pinned revision/keys, and
// the 3+ distinct DNS/operator origin policy. Then require EVERY origin
// to sign a fresh client challenge bound to its exact quorum bundle.
// All origins must still agree on the same >2/3 witness-signed checkpoint.
//
// Existing unauthenticated-source HTTPS constructors remain available for
// non-production fixtures. This strict constructor never falls back to
// accepting an unchallenged or cached bundle if a publisher is unavailable.
// It does not, by itself, establish checkpoint freshness or HSM custody.
func NewChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
	attestations []WitnessHTTPSOperatorAttestation,
	pinnedOperatorKeys [][]byte,
	pinnedWitnessKeys [][]byte,
	network types.NetworkID,
	validatorRoot types.Hash,
	expectedRevision uint64,
) (*MultiSourceWitnessFloor, error) {
	floor, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		attestations, pinnedOperatorKeys, pinnedWitnessKeys, network,
		validatorRoot, expectedRevision,
	)
	if err != nil {
		return nil, err
	}
	if len(floor.sources) != len(attestations) {
		return nil, ErrWitnessSigningIntent
	}
	for i, attestation := range attestations {
		quorum, ok := floor.sources[i].(*QuorumAuthenticatedWitnessFloor)
		if !ok || quorum == nil {
			return nil, ErrWitnessSigningIntent
		}
		source, ok := quorum.source.(*HTTPWitnessFloorSource)
		if !ok || source == nil {
			return nil, ErrWitnessSigningIntent
		}
		source.challengePolicy = &witnessHTTPSChallengePolicy{
			network:       network,
			validatorRoot: validatorRoot,
			operatorID:    attestation.OperatorID,
			revision:      expectedRevision,
			operatorKey:   append(ed25519.PublicKey(nil), attestation.OperatorPublicKey...),
		}
	}
	return floor, nil
}
