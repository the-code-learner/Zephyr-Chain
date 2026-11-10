package node

import (
	"bytes"
	"crypto/ed25519"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// WitnessHTTPSOperatorAttestation is an out-of-band signed, network-scoped
// inventory entry. OperatorPublicKey is a separately pinned INVENTORY key,
// NEVER a witness checkpoint-signing or private custody key. This attests
// the claimed endpoint and roster revision, not physical/legal independence,
// trusted DNS hosting, remote signer state or publication freshness.
type WitnessHTTPSOperatorAttestation struct {
	OperatorID        string
	Endpoint          string
	Revision          uint64
	OperatorPublicKey []byte
	Signature         []byte
}

// WitnessHTTPSOperatorAttestationDigest binds the full inventory entry to
// the exact independently pinned network/validator-root and operator key.
// The expected revision MUST be pinned outside potentially rolled-back
// local storage; otherwise a previously signed roster can be replayed.
func WitnessHTTPSOperatorAttestationDigest(
	a WitnessHTTPSOperatorAttestation,
	network types.NetworkID,
	validatorRoot types.Hash,
) ([32]byte, error) {
	if !validWitnessPublicationOperatorID(a.OperatorID) ||
		len(a.Endpoint) == 0 || len(a.Endpoint) > 512 ||
		a.Revision == 0 || len(a.OperatorPublicKey) != ed25519.PublicKeySize ||
		types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validatorRoot)) {
		return [32]byte{}, ErrWitnessSigningIntent
	}
	var w codec.Writer
	w.Fixed(network[:])
	w.Fixed(validatorRoot[:])
	w.U64(a.Revision)
	w.String(a.OperatorID)
	w.String(a.Endpoint)
	w.Fixed(a.OperatorPublicKey)
	return codec.DomainHash("zephyr/witness-https-operator-inventory/v1", w.BytesCopy()), nil
}

// NewAttestedMultiOperatorHTTPSWitnessFloor verifies independent inventory
// signatures BEFORE allocating any HTTPS retrieval path. Each entry must be
// signed by a unique preapproved operator identity key; the set of pinned
// keys must be used EXACTLY once. Inventory keys cannot overlap witness
// quorum keys (separate authority domains). The caller also pins the
// expected monotonic roster revision outside the restored witness database.
//
// After inventory verification the strict HTTPS constructor independently
// enforces 3+ unique DNS hosts and 4+ pinned witness keys, then verifies
// signatures/quorum and all-origin agreement on EVERY checkpoint read.
// Cryptographic inventory proof does NOT verify that distinct operator
// keys are controlled by separate people, hardware, hosts or legal entities.
func NewAttestedMultiOperatorHTTPSWitnessFloor(
	attestations []WitnessHTTPSOperatorAttestation,
	pinnedOperatorKeys [][]byte,
	pinnedWitnessKeys [][]byte,
	network types.NetworkID,
	validatorRoot types.Hash,
	expectedRevision uint64,
) (*MultiSourceWitnessFloor, error) {
	if len(attestations) < 3 || len(attestations) > 8 ||
		len(pinnedOperatorKeys) != len(attestations) ||
		len(pinnedWitnessKeys) < 4 || expectedRevision == 0 {
		return nil, ErrWitnessSigningIntent
	}
	pinned := make(map[string]struct{}, len(pinnedOperatorKeys))
	for _, key := range pinnedOperatorKeys {
		if len(key) != ed25519.PublicKeySize {
			return nil, ErrWitnessSigningIntent
		}
		k := string(key)
		if _, duplicate := pinned[k]; duplicate {
			return nil, ErrWitnessSigningIntent
		}
		// The inventory issuer must never share the very same key with a
		// witness quorum signer, even if they claim different identities.
		for _, witnessKey := range pinnedWitnessKeys {
			if bytes.Equal(key, witnessKey) {
				return nil, ErrWitnessSigningIntent
			}
		}
		pinned[k] = struct{}{}
	}
	used := make(map[string]struct{}, len(attestations))
	publications := make([]WitnessHTTPSPublication, 0, len(attestations))
	for _, attestation := range attestations {
		key := string(attestation.OperatorPublicKey)
		if attestation.Revision != expectedRevision ||
			len(attestation.OperatorPublicKey) != ed25519.PublicKeySize ||
			len(attestation.Signature) != ed25519.SignatureSize {
			return nil, ErrWitnessSigningIntent
		}
		if _, ok := pinned[key]; !ok {
			return nil, ErrWitnessSigningIntent
		}
		if _, duplicate := used[key]; duplicate {
			return nil, ErrWitnessSigningIntent
		}
		digest, err := WitnessHTTPSOperatorAttestationDigest(attestation, network, validatorRoot)
		if err != nil || !ed25519.Verify(ed25519.PublicKey(attestation.OperatorPublicKey), digest[:], attestation.Signature) {
			return nil, ErrWitnessSigningIntent
		}
		used[key] = struct{}{}
		publications = append(publications, WitnessHTTPSPublication{
			OperatorID: attestation.OperatorID,
			Endpoint:   attestation.Endpoint,
		})
	}
	if len(used) != len(pinned) {
		return nil, ErrWitnessSigningIntent
	}
	return NewProvisionedMultiOperatorHTTPSWitnessFloor(
		publications, pinnedWitnessKeys, network, validatorRoot,
	)
}
