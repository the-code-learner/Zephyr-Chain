package node

import (
	"crypto/ed25519"
	"crypto/subtle"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// NewPinnedDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor
// requires TWO independent authorization steps for each online publisher:
// (1) a valid OFFLINE operator-issuer signature over its key delegation,
// (2) an exact ONLINE key pin provisioned through an authenticated channel
// OUTSIDE that operator-controlled signed delegation bundle.
//
// An operator issuer could sign two different delegation keys at the same
// roster revision, or a restored backup could replay an older signed key.
// A matching externally provisioned key pin refuses a substituted key even
// if its delegation signature is cryptographically valid. Pins are indexed
// in the exact, separately signed 3+ operator inventory order.
//
// This constructor DOES NOT prove the pinning channel is independent, that
// revocations are globally monotonic, or that operators/HSMs are genuinely
// separate. Pins and the expected roster revision must be authenticated,
// reviewed and rollback-protected by the deploying organization.
func NewPinnedDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
	attestations []WitnessHTTPSOperatorAttestation,
	pinnedOperatorKeys [][]byte,
	pinnedWitnessKeys [][]byte,
	delegations []WitnessHTTPSPublicationDelegation,
	pinnedPublicationKeys [][]byte,
	network types.NetworkID,
	validatorRoot types.Hash,
	expectedRevision uint64,
) (*MultiSourceWitnessFloor, error) {
	if len(attestations) < 3 || len(attestations) > 8 ||
		len(delegations) != len(attestations) ||
		len(pinnedPublicationKeys) != len(attestations) ||
		expectedRevision == 0 {
		return nil, ErrWitnessSigningIntent
	}
	for i, delegation := range delegations {
		expected := pinnedPublicationKeys[i]
		if len(expected) != ed25519.PublicKeySize ||
			len(delegation.PublicationPublicKey) != ed25519.PublicKeySize ||
			subtle.ConstantTimeCompare(expected, delegation.PublicationPublicKey) != 1 {
			return nil, ErrWitnessSigningIntent
		}
	}
	return NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
		attestations, pinnedOperatorKeys, pinnedWitnessKeys, delegations,
		network, validatorRoot, expectedRevision,
	)
}
