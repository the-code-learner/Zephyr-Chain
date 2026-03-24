package ledger

import "github.com/zephyr-chain/zephyr-chain/internal/consensus"

func findProposal(proposals []consensus.Proposal, height uint64, round uint64) *consensus.Proposal {
	for _, proposal := range proposals {
		if proposal.Height == height && proposal.Round == round {
			cloned := cloneProposal(proposal)
			return &cloned
		}
	}
	return nil
}

func latestProposalForHeight(proposals []consensus.Proposal, height uint64) *consensus.Proposal {
	for index := len(proposals) - 1; index >= 0; index-- {
		proposal := proposals[index]
		if proposal.Height != height {
			continue
		}
		cloned := cloneProposal(proposal)
		return &cloned
	}
	return nil
}

func proposalsForHeight(proposals []consensus.Proposal, height uint64) []consensus.Proposal {
	filtered := make([]consensus.Proposal, 0)
	for _, proposal := range proposals {
		if proposal.Height != height {
			continue
		}
		filtered = append(filtered, cloneProposal(proposal))
	}
	return filtered
}
func findVoteByValidator(votes []VoteRecord, height uint64, round uint64, voter string) *consensus.Vote {
	for _, vote := range votes {
		if vote.Vote.Height == height && vote.Vote.Round == round && vote.Vote.Voter == voter {
			cloned := vote.Vote
			return &cloned
		}
	}
	return nil
}

func latestVoteByValidatorForHeight(votes []VoteRecord, height uint64, voter string) *consensus.Vote {
	for index := len(votes) - 1; index >= 0; index-- {
		vote := votes[index]
		if vote.Vote.Height != height || vote.Vote.Voter != voter {
			continue
		}
		cloned := vote.Vote
		return &cloned
	}
	return nil
}

func hasVoteFromValidator(votes []VoteRecord, height uint64, round uint64, voter string) bool {
	for _, vote := range votes {
		if vote.Vote.Height == height && vote.Vote.Round == round && vote.Vote.Voter == voter {
			return true
		}
	}
	return false
}

func certificatesForHeight(certificates []CommitCertificate, height uint64) []CommitCertificate {
	filtered := make([]CommitCertificate, 0)
	for _, certificate := range certificates {
		if certificate.Height != height {
			continue
		}
		filtered = append(filtered, cloneCommitCertificate(certificate))
	}
	return filtered
}
func latestCertificateForHeightRound(certificates []CommitCertificate, height uint64, round uint64) *CommitCertificate {
	for index := len(certificates) - 1; index >= 0; index-- {
		certificate := certificates[index]
		if certificate.Height != height || certificate.Round != round {
			continue
		}
		cloned := cloneCommitCertificate(certificate)
		return &cloned
	}
	return nil
}
