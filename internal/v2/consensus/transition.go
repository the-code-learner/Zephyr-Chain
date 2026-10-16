package consensus

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
)

// VerifyCertifiedTransition proves that a currently trusted validator set
// finalized a header which authorizes next as the validator set for the next
// height. This is the trust-chain primitive used by Citizen checkpoints.
func VerifyCertifiedTransition(header sharding.GlobalHeader, certificate Certificate, current, next ValidatorSet) error {
	currentRoot, err := current.Root()
	if err != nil || current.Network != header.Network || currentRoot != header.ValidatorRoot {
		return ErrValidatorSet
	}
	if certificate.Network != header.Network || certificate.Height != header.Height || certificate.HeaderHash != HeaderConsensusHash(header) || header.CertificateHash != certificate.Hash() {
		return ErrCertificate
	}
	if err := current.VerifyCertificate(certificate); err != nil {
		return err
	}
	nextRoot, err := next.Root()
	if err != nil || next.Network != header.Network || nextRoot != header.EffectiveNextValidatorRoot() {
		return ErrValidatorSet
	}
	return nil
}
