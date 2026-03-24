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

func hasVoteFromValidator(votes []VoteRecord, height uint64, round uint64, voter string) bool {
	for _, vote := range votes {
		if vote.Vote.Height == height && vote.Vote.Round == round && vote.Vote.Voter == voter {
			return true
		}
	}
	return false
}
