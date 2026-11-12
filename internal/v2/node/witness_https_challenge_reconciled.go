package node

import (
	"context"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// NewReconciledWitnessHTTPSChallengePublicationHandler installs an opt-in
// fail-closed publication fence. In addition to independently verifying
// signed witness quorum and genesis-anchored QC finality, it requires an
// AUTHENTICATED external high-water source to return the EXACT candidate
// checkpoint on EVERY request, before the online signing provider is called.
// The external floor must support context cancellation; a blocking legacy
// synchronous adapter is not accepted in this strict mode.
//
// Deployers MUST provision the external floor using independent operator
// identities, pins and failure domains. Supplying the same untrusted
// source behind this interface is circular and does NOT establish freshness.
// This constructor is NOT a hardware-backed globally monotonic authority:
// coordinated rollback of the floor and publisher, process restart,
// compromised host or a dishonest operator remain external release blockers.
func NewReconciledWitnessHTTPSChallengePublicationHandler(
	source SignedWitnessFloorSource,
	verifier WitnessFinalityVerifier,
	signer WitnessHTTPSPublicationSigner,
	network types.NetworkID,
	validatorRoot types.Hash,
	operatorID string,
	revision uint64,
	offlineInventoryKey []byte,
	publicationKey []byte,
	witnessKeys [][]byte,
	external WitnessExternalFloorVerifier,
) (*WitnessHTTPSChallengePublicationHandler, error) {
	if external == nil {
		return nil, ErrWitnessSigningIntent
	}
	contextual, ok := external.(interface {
		AuthenticatedWitnessFloorContext(context.Context) (FinalityCheckpoint, error)
	})
	if !ok {
		return nil, ErrWitnessSigningIntent
	}
	h, err := NewWitnessHTTPSChallengePublicationHandler(
		source, verifier, signer, network, validatorRoot, operatorID,
		revision, offlineInventoryKey, publicationKey, witnessKeys,
	)
	if err != nil {
		return nil, err
	}
	h.externalFloor = contextual
	return h, nil
}
