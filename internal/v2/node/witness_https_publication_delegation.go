package node

import (
	"crypto/ed25519"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// WitnessHTTPSPublicationDelegation is an OFFLINE operator-issuer-signed
// authorization for one separate ONLINE HTTPS publication Ed25519 key.
// This authorization is not a witness QC key and never grants custody
// authority or a right to create arbitrary finalized checkpoints.
type WitnessHTTPSPublicationDelegation struct {
	OperatorID           string
	Revision             uint64
	PublicationPublicKey []byte
	Signature            []byte
}

// WitnessHTTPSPublicationDelegationDigest binds one online publication key to
// the full independently pinned genesis/root, operator and inventory revision.
// The offline inventory issuer signs these bytes through its own isolated
// signing ceremony; the issuer private key MUST NOT run on the HTTPS host.
func WitnessHTTPSPublicationDelegationDigest(
	delegation WitnessHTTPSPublicationDelegation,
	network types.NetworkID,
	validatorRoot types.Hash,
) ([32]byte, error) {
	if types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validatorRoot)) ||
		!validWitnessPublicationOperatorID(delegation.OperatorID) ||
		delegation.Revision == 0 ||
		len(delegation.PublicationPublicKey) != ed25519.PublicKeySize {
		return [32]byte{}, ErrWitnessSigningIntent
	}
	var writer codec.Writer
	writer.Fixed(network[:])
	writer.Fixed(validatorRoot[:])
	writer.String(delegation.OperatorID)
	writer.U64(delegation.Revision)
	writer.Fixed(delegation.PublicationPublicKey)
	return codec.DomainHash("zephyr/witness-https-online-publication-delegation/v1", writer.BytesCopy()), nil
}

// NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor requires
// an independently signed roster AND exactly one separately signed
// publication-key delegation per operator, in the identical roster order.
// The offline inventory signer is checked against a separately pinned
// identity; every online public key is scoped to its operator and expected
// externally pinned revision. The underlying constructor additionally
// checks key-role separation, canonical HTTPS/unique hosts, witness quorum,
// challenge response, and all-source checkpoint agreement.
//
// A signed delegation alone is not revocation, a globally monotonic policy,
// a key ceremony, proof of separate operators, or proof of fresh QC finality.
// Restoring an older expected revision defeats rollback protection; pin that
// revision in a separately authenticated nonrollbackable trust domain.
func NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
	attestations []WitnessHTTPSOperatorAttestation,
	pinnedOperatorKeys [][]byte,
	pinnedWitnessKeys [][]byte,
	delegations []WitnessHTTPSPublicationDelegation,
	network types.NetworkID,
	validatorRoot types.Hash,
	expectedRevision uint64,
) (*MultiSourceWitnessFloor, error) {
	if len(attestations) < 3 || len(attestations) > 8 ||
		len(delegations) != len(attestations) || expectedRevision == 0 {
		return nil, ErrWitnessSigningIntent
	}
	publicationKeys := make([][]byte, 0, len(delegations))
	for i, attestation := range attestations {
		delegation := delegations[i]
		if delegation.OperatorID != attestation.OperatorID ||
			delegation.Revision != expectedRevision ||
			len(attestation.OperatorPublicKey) != ed25519.PublicKeySize ||
			len(delegation.Signature) != ed25519.SignatureSize {
			return nil, ErrWitnessSigningIntent
		}
		digest, err := WitnessHTTPSPublicationDelegationDigest(delegation, network, validatorRoot)
		if err != nil || !ed25519.Verify(attestation.OperatorPublicKey, digest[:], delegation.Signature) {
			return nil, ErrWitnessSigningIntent
		}
		publicationKeys = append(publicationKeys, append([]byte(nil), delegation.PublicationPublicKey...))
	}
	return NewChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
		attestations, pinnedOperatorKeys, pinnedWitnessKeys, publicationKeys,
		network, validatorRoot, expectedRevision,
	)
}
