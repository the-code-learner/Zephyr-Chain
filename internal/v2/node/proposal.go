package node

import (
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
)

// VerifyProposalAgainstCandidate is the pre-vote safety gate. Cryptographic
// proposal validity is necessary but not sufficient: an honest validator only
// votes when its independently executed candidate produces the exact same
// consensus GlobalHeader.
func VerifyProposalAgainstCandidate(candidate Candidate, proposal v2consensus.Proposal, validators v2consensus.ValidatorSet) error {
	if err := validators.VerifyProposal(proposal); err != nil {
		return err
	}
	if v2consensus.HeaderConsensusHash(candidate.Header) != v2consensus.HeaderConsensusHash(proposal.Header) {
		return ErrCandidateState
	}
	return nil
}
